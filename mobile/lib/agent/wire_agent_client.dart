import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/bearer_authentication.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

final class WireAgentClient implements AgentClient, AgentPracticeAvailability {
  factory WireAgentClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
    Duration pollInterval = const Duration(seconds: 1),
    int maxRunPollAttempts = 75,
    int maxMessagePollAttempts = 4,
    Duration requestTimeout = const Duration(seconds: 75),
  }) {
    if (pollInterval.isNegative ||
        maxRunPollAttempts < 1 ||
        maxMessagePollAttempts < 1 ||
        requestTimeout <= Duration.zero) {
      throw ArgumentError('Agent client polling configuration is invalid.');
    }
    final ownsTransport = transport == null;
    return WireAgentClient._(
      baseUri,
      TrustedIdentityHttpOrigin(baseUri),
      credentialProvider,
      invalidateSession,
      transport ?? _IoAgentHttpTransport(requestTimeout: requestTimeout),
      ownsTransport,
      requestTimeout,
      pollInterval,
      maxRunPollAttempts,
      maxMessagePollAttempts,
    );
  }

  WireAgentClient._(
    this._baseUri,
    this._trustedOrigin,
    this._credentialProvider,
    this._invalidateSession,
    this._transport,
    this._ownsTransport,
    this._requestTimeout,
    this._pollInterval,
    this._maxRunPollAttempts,
    this._maxMessagePollAttempts,
  );

  final Uri _baseUri;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  final AuthSessionCredentialProvider _credentialProvider;
  final AuthSessionInvalidator _invalidateSession;
  final bool _ownsTransport;
  final Duration _requestTimeout;
  final Duration _pollInterval;
  final int _maxRunPollAttempts;
  final int _maxMessagePollAttempts;

  IdentityHttpTransport _transport;
  int _accountGeneration = 0;
  final Set<Future<void>> _inFlightOperations = <Future<void>>{};
  final Map<String, _FailedRun> _failedRuns = <String, _FailedRun>{};
  final Set<String> _ambiguousSubmissions = <String>{};
  Future<void>? _cleanupFuture;

  @override
  bool get supportsPracticeFlow => false;

  @override
  Future<void> clearAccountState() {
    final existing = _cleanupFuture;
    if (existing != null) {
      return existing;
    }
    final operation = _performAccountCleanup();
    _cleanupFuture = operation;
    return operation.whenComplete(() {
      if (identical(_cleanupFuture, operation)) {
        _cleanupFuture = null;
      }
    });
  }

  Future<void> _performAccountCleanup() async {
    _accountGeneration++;
    _failedRuns.clear();
    _ambiguousSubmissions.clear();
    final staleOperations = List<Future<void>>.of(_inFlightOperations);
    if (_ownsTransport) {
      (_transport as _IoAgentHttpTransport).close(force: true);
    }
    await Future.wait(staleOperations);
    if (_ownsTransport) {
      _transport = _IoAgentHttpTransport(requestTimeout: _requestTimeout);
    }
  }

  @override
  Future<AgentThreadSnapshot> restoreThread() {
    return _runAccountOperation((generation) async {
      final listResponse = await _send(
        generation: generation,
        method: 'GET',
        path: '/v1/agent-threads',
      );
      _requireStatus(listResponse, const <int>{HttpStatus.ok});
      final threads = _decodeThreadList(listResponse.body);
      _requireCurrentGeneration(generation);

      final thread = threads.isEmpty
          ? await _createThread(generation)
          : threads.first;
      final messages = await _listMessages(
        generation: generation,
        threadId: thread.id,
      );
      _requireCurrentGeneration(generation);
      return AgentThreadSnapshot(
        threadId: thread.id,
        messages: [for (final message in messages) message.presentation],
      );
    });
  }

  Future<_WireThread> _createThread(int generation) async {
    final response = await _send(
      generation: generation,
      method: 'POST',
      path: '/v1/agent-threads',
      body: const <String, Object?>{},
    );
    _requireStatus(response, const <int>{HttpStatus.created});
    return _decodeThread(response.body);
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) {
    return _runAccountOperation((generation) async {
      _requireUuid(threadId);
      _requireClientIdentity(clientMessageId);
      _requireContent(text);
      final operationKey = '$threadId\u{0}$clientMessageId';
      final failedRun = _failedRuns[operationKey];
      if (failedRun != null && failedRun.content != text) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.conflict,
          errorCode: 'idempotency_key_conflict',
        );
      }

      late _WireRun initialRun;
      if (failedRun != null) {
        initialRun = await _retryRun(
          generation: generation,
          failedRun: failedRun,
        );
      } else {
        try {
          initialRun = await _submitRun(
            generation: generation,
            threadId: threadId,
            text: text,
            clientMessageId: clientMessageId,
          );
        } on AgentClientException catch (error) {
          if (error.kind == AgentClientFailureKind.network) {
            _requireCurrentGeneration(generation);
            _ambiguousSubmissions.add(operationKey);
          }
          rethrow;
        }
        if (_ambiguousSubmissions.contains(operationKey)) {
          if (initialRun.status == _WireRunStatus.failed &&
              initialRun.failureRetryable) {
            _ambiguousSubmissions.remove(operationKey);
            final recoveredFailure = _FailedRun(
              runId: initialRun.id,
              threadId: threadId,
              inputMessageId: initialRun.inputMessageId,
              content: text,
            );
            _failedRuns[operationKey] = recoveredFailure;
            initialRun = await _retryRun(
              generation: generation,
              failedRun: recoveredFailure,
            );
          }
        }
      }
      final terminalRun = await _pollUntilTerminal(
        generation: generation,
        initialRun: initialRun,
      );
      _requireCurrentGeneration(generation);
      var resolvedRun = terminalRun;

      if (_ambiguousSubmissions.contains(operationKey) &&
          resolvedRun.status == _WireRunStatus.failed &&
          resolvedRun.failureRetryable) {
        _ambiguousSubmissions.remove(operationKey);
        final recoveredFailure = _FailedRun(
          runId: resolvedRun.id,
          threadId: threadId,
          inputMessageId: resolvedRun.inputMessageId,
          content: text,
        );
        _failedRuns[operationKey] = recoveredFailure;
        resolvedRun = await _pollUntilTerminal(
          generation: generation,
          initialRun: await _retryRun(
            generation: generation,
            failedRun: recoveredFailure,
          ),
        );
        _requireCurrentGeneration(generation);
      }
      _ambiguousSubmissions.remove(operationKey);

      if (resolvedRun.status == _WireRunStatus.failed) {
        if (resolvedRun.failureRetryable) {
          _failedRuns[operationKey] = _FailedRun(
            runId: resolvedRun.id,
            threadId: threadId,
            inputMessageId: resolvedRun.inputMessageId,
            content: text,
          );
        } else {
          _failedRuns.remove(operationKey);
        }
        throw AgentClientException(
          kind: AgentClientFailureKind.runFailed,
          errorCode: resolvedRun.failureKind,
          retryable: resolvedRun.failureRetryable,
        );
      }

      _failedRuns.remove(operationKey);
      return _loadCompletedExchange(generation: generation, run: resolvedRun);
    });
  }

  Future<_WireRun> _submitRun({
    required int generation,
    required String threadId,
    required String text,
    required String clientMessageId,
  }) async {
    final response = await _send(
      generation: generation,
      method: 'POST',
      path: '/v1/agent-threads/$threadId/runs',
      body: <String, Object?>{
        'client_message_id': clientMessageId,
        'content': text,
      },
    );
    _requireStatus(response, const <int>{
      HttpStatus.created,
      HttpStatus.accepted,
    });
    final run = _decodeRun(response.body);
    _validateRunWriteStatus(response.statusCode, run);
    if (run.threadId != threadId) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    return run;
  }

  Future<_WireRun> _retryRun({
    required int generation,
    required _FailedRun failedRun,
  }) async {
    final retryClientId = 'retry:${failedRun.runId}';
    _requireClientIdentity(retryClientId);
    final response = await _send(
      generation: generation,
      method: 'POST',
      path: '/v1/agent-runs/${failedRun.runId}/retries',
      body: <String, Object?>{'client_retry_id': retryClientId},
    );
    _requireStatus(response, const <int>{
      HttpStatus.created,
      HttpStatus.accepted,
    });
    final run = _decodeRun(response.body);
    _validateRunWriteStatus(response.statusCode, run);
    if (run.threadId != failedRun.threadId ||
        run.inputMessageId != failedRun.inputMessageId ||
        run.retryOfRunId != failedRun.runId ||
        run.clientRetryId != retryClientId) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    return run;
  }

  Future<_WireRun> _pollUntilTerminal({
    required int generation,
    required _WireRun initialRun,
  }) async {
    var run = initialRun;
    for (var attempt = 0; attempt < _maxRunPollAttempts; attempt++) {
      if (run.isTerminal) {
        return run;
      }
      if (attempt + 1 == _maxRunPollAttempts) {
        break;
      }
      await _waitForPoll(generation);
      final response = await _send(
        generation: generation,
        method: 'GET',
        path: '/v1/agent-runs/${run.id}',
      );
      _requireStatus(response, const <int>{HttpStatus.ok});
      final restored = _decodeRun(response.body);
      if (restored.id != run.id ||
          restored.threadId != run.threadId ||
          restored.inputMessageId != run.inputMessageId) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidResponse,
          retryable: true,
        );
      }
      run = restored;
    }
    throw const AgentClientException(
      kind: AgentClientFailureKind.pollingTimedOut,
      retryable: true,
    );
  }

  Future<AgentExchange> _loadCompletedExchange({
    required int generation,
    required _WireRun run,
  }) async {
    final assistantMessageId = run.assistantMessageId;
    if (assistantMessageId == null) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    for (var attempt = 0; attempt < _maxMessagePollAttempts; attempt++) {
      final messages = await _listMessages(
        generation: generation,
        threadId: run.threadId,
      );
      _WireMessage? userMessage;
      _WireMessage? assistantMessage;
      for (final message in messages) {
        if (message.id == run.inputMessageId) {
          userMessage = message;
        }
        if (message.id == assistantMessageId) {
          assistantMessage = message;
        }
      }
      if (userMessage != null &&
          userMessage.role == AgentMessageRole.user &&
          assistantMessage != null &&
          assistantMessage.role == AgentMessageRole.assistant &&
          assistantMessage.producedByRunId == run.id) {
        return AgentExchange(
          userMessage: userMessage.presentation,
          assistantMessage: assistantMessage.presentation,
        );
      }
      if (attempt + 1 < _maxMessagePollAttempts) {
        await _waitForPoll(generation);
      }
    }
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidResponse,
      retryable: true,
    );
  }

  Future<List<_WireMessage>> _listMessages({
    required int generation,
    required String threadId,
  }) async {
    final response = await _send(
      generation: generation,
      method: 'GET',
      path: '/v1/agent-threads/$threadId/messages',
    );
    _requireStatus(response, const <int>{HttpStatus.ok});
    return _decodeMessageList(response.body, expectedThreadId: threadId);
  }

  Future<void> _waitForPoll(int generation) async {
    if (_pollInterval != Duration.zero) {
      await Future<void>.delayed(_pollInterval);
    }
    _requireCurrentGeneration(generation);
  }

  @override
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) {
    return Future<AgentSceneStart>.error(_practiceUnavailable);
  }

  @override
  Future<String> transcribeTurn({
    required String threadId,
    required int turnNumber,
    required String clientTurnId,
  }) {
    return Future<String>.error(_practiceUnavailable);
  }

  @override
  Future<AgentExchange> submitPracticeTurn({
    required String threadId,
    required AgentScene scene,
    required int turnNumber,
    required String transcript,
    required String clientTurnId,
  }) {
    return Future<AgentExchange>.error(_practiceUnavailable);
  }

  @override
  Future<AgentReview> createReview({
    required String threadId,
    required AgentScene scene,
    required String clientReviewId,
  }) {
    return Future<AgentReview>.error(_practiceUnavailable);
  }

  Future<IdentityHttpResponse> _send({
    required int generation,
    required String method,
    required String path,
    Map<String, Object?>? body,
  }) async {
    _requireCurrentGeneration(generation);
    final credential = _credentialProvider();
    if (credential == null) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
        errorCode: 'authentication_required',
      );
    }
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(
      uri,
      sessionToken: credential.sessionToken,
    );
    final transport = _transport;
    late final IdentityHttpResponse response;
    try {
      response = await transport.send(
        method: method,
        uri: uri,
        headers: <String, String>{
          HttpHeaders.acceptHeader: ContentType.json.mimeType,
          if (body != null)
            HttpHeaders.contentTypeHeader: ContentType.json.mimeType,
          HttpHeaders.authorizationHeader: bearerAuthorizationValue(
            credential.sessionToken,
          ),
        },
        body: body == null ? null : jsonEncode(body),
      );
    } on AgentClientException {
      _requireCurrentGeneration(generation);
      rethrow;
    } on TimeoutException {
      _requireCurrentGeneration(generation);
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      _requireCurrentGeneration(generation);
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      _requireCurrentGeneration(generation);
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on IOException {
      _requireCurrentGeneration(generation);
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } catch (_) {
      _requireCurrentGeneration(generation);
      throw const AgentClientException(kind: AgentClientFailureKind.unexpected);
    }
    _requireCurrentGeneration(generation);
    if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
      throw const AgentClientOperationCancelled();
    }
    if (response.statusCode == HttpStatus.unauthorized) {
      unawaited(_invalidateCapturedCredential(credential));
      throw _exceptionFor(response);
    }
    return response;
  }

  Future<void> _invalidateCapturedCredential(
    AuthSessionCredential credential,
  ) async {
    try {
      await _invalidateSession(
        expectedSessionToken: credential.sessionToken,
        expectedGeneration: credential.generation,
      );
    } catch (_) {
      // The request still fails closed. A later authenticated operation will
      // repeat validation without exposing the captured credential.
    }
  }

  void _requireStatus(
    IdentityHttpResponse response,
    Set<int> expectedStatuses,
  ) {
    if (!expectedStatuses.contains(response.statusCode)) {
      throw _exceptionFor(response);
    }
  }

  AgentClientException _exceptionFor(IdentityHttpResponse response) {
    String? decodedErrorCode;
    String? correlationId;
    try {
      final root = _strictObject(
        jsonDecode(response.body),
        allowed: const <String>{'error'},
        required: const <String>{'error'},
      );
      final error = _strictObject(
        root['error'],
        allowed: const <String>{
          'code',
          'message',
          'retryable',
          'correlation_id',
          'details',
        },
        required: const <String>{
          'code',
          'message',
          'retryable',
          'correlation_id',
        },
      );
      decodedErrorCode = _strictString(
        error['code'],
        minLength: 1,
        maxLength: 64,
      );
      _strictString(error['message'], minLength: 1, maxLength: 512);
      _strictBool(error['retryable']);
      correlationId = _strictString(
        error['correlation_id'],
        minLength: 1,
        maxLength: 128,
      );
    } catch (_) {
      decodedErrorCode = null;
      correlationId = null;
    }

    final errorCode = _normalizedAgentErrorCode(
      statusCode: response.statusCode,
      decodedErrorCode: decodedErrorCode,
    );
    final kind = switch (response.statusCode) {
      HttpStatus.badRequest => AgentClientFailureKind.invalidRequest,
      HttpStatus.unauthorized => AgentClientFailureKind.authenticationRequired,
      HttpStatus.notFound => AgentClientFailureKind.notFound,
      HttpStatus.conflict => AgentClientFailureKind.conflict,
      HttpStatus.tooManyRequests => AgentClientFailureKind.rateLimited,
      >= 500 => AgentClientFailureKind.server,
      _ => AgentClientFailureKind.unexpected,
    };
    final retryable =
        kind == AgentClientFailureKind.rateLimited ||
        kind == AgentClientFailureKind.server;
    return AgentClientException(
      kind: kind,
      statusCode: response.statusCode,
      errorCode: errorCode,
      retryable: retryable,
      correlationId: correlationId,
    );
  }

  String? _normalizedAgentErrorCode({
    required int statusCode,
    required String? decodedErrorCode,
  }) {
    final allowedCode = switch ((statusCode, decodedErrorCode)) {
      (HttpStatus.badRequest, 'invalid_request') => 'invalid_request',
      (HttpStatus.unauthorized, 'authentication_required') =>
        'authentication_required',
      (HttpStatus.notFound, 'resource_not_found') => 'resource_not_found',
      (HttpStatus.conflict, 'idempotency_key_conflict') =>
        'idempotency_key_conflict',
      (HttpStatus.conflict, 'resource_conflict') => 'resource_conflict',
      (HttpStatus.tooManyRequests, 'rate_limited') => 'rate_limited',
      (>= 500, 'internal_error') => 'internal_error',
      _ => null,
    };
    if (allowedCode != null) {
      return allowedCode;
    }

    return switch (statusCode) {
      HttpStatus.badRequest => 'invalid_request',
      HttpStatus.unauthorized => 'authentication_required',
      HttpStatus.notFound => 'resource_not_found',
      HttpStatus.conflict => 'resource_conflict',
      HttpStatus.tooManyRequests => 'rate_limited',
      >= 500 => 'internal_error',
      _ => null,
    };
  }

  Future<T> _runAccountOperation<T>(
    Future<T> Function(int generation) operation,
  ) async {
    final cleanup = _cleanupFuture;
    if (cleanup != null) {
      await cleanup;
    }
    final generation = _accountGeneration;
    final completion = Completer<void>();
    final marker = completion.future;
    _inFlightOperations.add(marker);
    try {
      return await operation(generation);
    } finally {
      _inFlightOperations.remove(marker);
      completion.complete();
    }
  }

  void _requireCurrentGeneration(int generation) {
    if (generation != _accountGeneration) {
      throw const AgentClientOperationCancelled();
    }
  }
}

const AgentClientException _practiceUnavailable = AgentClientException(
  kind: AgentClientFailureKind.unavailable,
);

final class _FailedRun {
  const _FailedRun({
    required this.runId,
    required this.threadId,
    required this.inputMessageId,
    required this.content,
  });

  final String runId;
  final String threadId;
  final String inputMessageId;
  final String content;
}

final class _WireThread {
  const _WireThread({required this.id});

  final String id;
}

final class _WireMessage {
  const _WireMessage({
    required this.id,
    required this.role,
    required this.content,
    required this.sequence,
    this.producedByRunId,
  });

  final String id;
  final AgentMessageRole role;
  final String content;
  final int sequence;
  final String? producedByRunId;

  AgentMessage get presentation =>
      AgentMessage(id: id, role: role, text: content);
}

enum _WireRunStatus { pending, running, completed, failed }

final class _WireRun {
  const _WireRun({
    required this.id,
    required this.threadId,
    required this.inputMessageId,
    required this.status,
    required this.failureRetryable,
    this.assistantMessageId,
    this.failureKind,
    this.retryOfRunId,
    this.clientRetryId,
  });

  final String id;
  final String threadId;
  final String inputMessageId;
  final _WireRunStatus status;
  final String? assistantMessageId;
  final String? failureKind;
  final bool failureRetryable;
  final String? retryOfRunId;
  final String? clientRetryId;

  bool get isTerminal =>
      status == _WireRunStatus.completed || status == _WireRunStatus.failed;
}

List<_WireThread> _decodeThreadList(String body) {
  return _decodeBody(body, (value) {
    final root = _strictObject(
      value,
      allowed: const <String>{'threads'},
      required: const <String>{'threads'},
    );
    final values = _strictList(root['threads'], maxLength: 1000);
    return [for (final item in values) _decodeThreadObject(item)];
  });
}

_WireThread _decodeThread(String body) {
  return _decodeBody(body, _decodeThreadObject);
}

_WireThread _decodeThreadObject(Object? value) {
  final object = _strictObject(
    value,
    allowed: const <String>{
      'thread_id',
      'active_matter_id',
      'created_at',
      'updated_at',
    },
    required: const <String>{'thread_id', 'created_at', 'updated_at'},
  );
  final id = _strictUuid(object['thread_id']);
  if (object['active_matter_id'] case final activeMatterId?) {
    _strictUuid(activeMatterId);
  }
  final createdAt = _strictDateTime(object['created_at']);
  final updatedAt = _strictDateTime(object['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const _InvalidAgentResponse();
  }
  return _WireThread(id: id);
}

List<_WireMessage> _decodeMessageList(
  String body, {
  required String expectedThreadId,
}) {
  return _decodeBody(body, (value) {
    final root = _strictObject(
      value,
      allowed: const <String>{'messages'},
      required: const <String>{'messages'},
    );
    final values = _strictList(root['messages'], maxLength: 10000);
    final result = <_WireMessage>[];
    final messageIds = <String>{};
    var previousSequence = 0;
    for (final value in values) {
      final message = _decodeMessageObject(
        value,
        expectedThreadId: expectedThreadId,
      );
      if (!messageIds.add(message.id) || message.sequence <= previousSequence) {
        throw const _InvalidAgentResponse();
      }
      previousSequence = message.sequence;
      result.add(message);
    }
    return result;
  });
}

_WireMessage _decodeMessageObject(
  Object? value, {
  required String expectedThreadId,
}) {
  final object = _strictObject(
    value,
    allowed: const <String>{
      'message_id',
      'thread_id',
      'sequence',
      'role',
      'client_message_id',
      'produced_by_run_id',
      'content',
      'created_at',
    },
    required: const <String>{
      'message_id',
      'thread_id',
      'sequence',
      'role',
      'content',
      'created_at',
    },
  );
  final id = _strictUuid(object['message_id']);
  if (_strictUuid(object['thread_id']) != expectedThreadId) {
    throw const _InvalidAgentResponse();
  }
  final sequence = _strictInt(object['sequence'], minimum: 1);
  final roleValue = _strictString(object['role'], minLength: 1, maxLength: 16);
  final role = switch (roleValue) {
    'user' => AgentMessageRole.user,
    'assistant' => AgentMessageRole.assistant,
    _ => throw const _InvalidAgentResponse(),
  };
  final content = _strictString(
    object['content'],
    minLength: 1,
    maxLength: 4096,
  );
  if (content.trim().isEmpty || utf8.encode(content).length > 16384) {
    throw const _InvalidAgentResponse();
  }
  _strictDateTime(object['created_at']);
  final clientMessageId = object['client_message_id'] == null
      ? null
      : _strictClientIdentity(object['client_message_id']);
  final producedByRunId = object['produced_by_run_id'] == null
      ? null
      : _strictUuid(object['produced_by_run_id']);
  if ((role == AgentMessageRole.user &&
          (clientMessageId == null || producedByRunId != null)) ||
      (role == AgentMessageRole.assistant &&
          (clientMessageId != null || producedByRunId == null))) {
    throw const _InvalidAgentResponse();
  }
  return _WireMessage(
    id: id,
    role: role,
    content: content,
    sequence: sequence,
    producedByRunId: producedByRunId,
  );
}

_WireRun _decodeRun(String body) {
  return _decodeBody(body, (value) {
    final object = _strictObject(
      value,
      allowed: const <String>{
        'run_id',
        'thread_id',
        'input_message_id',
        'attempt',
        'retry_of_run_id',
        'client_retry_id',
        'status',
        'requested_provider',
        'requested_model',
        'max_output_tokens',
        'assistant_message_id',
        'provider_completion_id',
        'provider_model',
        'finish_reason',
        'usage',
        'failure',
        'created_at',
        'started_at',
        'completed_at',
        'updated_at',
      },
      required: const <String>{
        'run_id',
        'thread_id',
        'input_message_id',
        'attempt',
        'status',
        'requested_provider',
        'requested_model',
        'max_output_tokens',
        'created_at',
        'updated_at',
      },
    );
    final id = _strictUuid(object['run_id']);
    final threadId = _strictUuid(object['thread_id']);
    final inputMessageId = _strictUuid(object['input_message_id']);
    _strictInt(object['attempt'], minimum: 1);
    final retryOfRunId = object['retry_of_run_id'] == null
        ? null
        : _strictUuid(object['retry_of_run_id']);
    final clientRetryId = object['client_retry_id'] == null
        ? null
        : _strictClientIdentity(object['client_retry_id']);
    if ((retryOfRunId == null) != (clientRetryId == null)) {
      throw const _InvalidAgentResponse();
    }
    final statusValue = _strictString(
      object['status'],
      minLength: 1,
      maxLength: 16,
    );
    final status = switch (statusValue) {
      'pending' => _WireRunStatus.pending,
      'running' => _WireRunStatus.running,
      'completed' => _WireRunStatus.completed,
      'failed' => _WireRunStatus.failed,
      _ => throw const _InvalidAgentResponse(),
    };
    _strictPatternString(
      object['requested_provider'],
      pattern: _providerPattern,
      maxLength: 64,
    );
    _strictClientIdentity(object['requested_model']);
    _strictInt(object['max_output_tokens'], minimum: 1);
    final createdAt = _strictDateTime(object['created_at']);
    final updatedAt = _strictDateTime(object['updated_at']);
    if (updatedAt.isBefore(createdAt)) {
      throw const _InvalidAgentResponse();
    }
    final startedAt = object['started_at'] == null
        ? null
        : _strictDateTime(object['started_at']);
    final completedAt = object['completed_at'] == null
        ? null
        : _strictDateTime(object['completed_at']);
    if (startedAt != null && startedAt.isBefore(createdAt) ||
        completedAt != null && startedAt == null ||
        completedAt != null && completedAt.isBefore(startedAt!)) {
      throw const _InvalidAgentResponse();
    }

    final assistantMessageId = object['assistant_message_id'] == null
        ? null
        : _strictUuid(object['assistant_message_id']);
    final failureObject = object['failure'] == null
        ? null
        : _strictObject(
            object['failure'],
            allowed: const <String>{'kind', 'retryable'},
            required: const <String>{'kind', 'retryable'},
          );
    final failureKind = failureObject == null
        ? null
        : _strictPatternString(
            failureObject['kind'],
            pattern: _failureKindPattern,
            maxLength: 64,
          );
    if (failureKind != null && !_knownRunFailureKinds.contains(failureKind)) {
      throw const _InvalidAgentResponse();
    }
    final failureRetryable = failureObject == null
        ? false
        : _strictBool(failureObject['retryable']);

    switch (status) {
      case _WireRunStatus.completed:
        if (assistantMessageId == null ||
            failureObject != null ||
            startedAt == null ||
            completedAt == null) {
          throw const _InvalidAgentResponse();
        }
        _strictClientIdentity(object['provider_completion_id']);
        _strictClientIdentity(object['provider_model']);
        final finishReason = _strictString(
          object['finish_reason'],
          minLength: 1,
          maxLength: 16,
        );
        if (finishReason != 'stop' && finishReason != 'length') {
          throw const _InvalidAgentResponse();
        }
        final usage = _strictObject(
          object['usage'],
          allowed: const <String>{
            'input_tokens',
            'output_tokens',
            'total_tokens',
          },
          required: const <String>{
            'input_tokens',
            'output_tokens',
            'total_tokens',
          },
        );
        _strictInt(usage['input_tokens'], minimum: 0);
        _strictInt(usage['output_tokens'], minimum: 0);
        _strictInt(usage['total_tokens'], minimum: 0);
      case _WireRunStatus.failed:
        if (failureObject == null ||
            assistantMessageId != null ||
            startedAt == null ||
            completedAt == null ||
            object['usage'] != null) {
          throw const _InvalidAgentResponse();
        }
      case _WireRunStatus.pending:
        if (assistantMessageId != null ||
            failureObject != null ||
            startedAt != null ||
            completedAt != null ||
            object['usage'] != null) {
          throw const _InvalidAgentResponse();
        }
      case _WireRunStatus.running:
        if (assistantMessageId != null ||
            failureObject != null ||
            startedAt == null ||
            completedAt != null ||
            object['usage'] != null) {
          throw const _InvalidAgentResponse();
        }
    }

    return _WireRun(
      id: id,
      threadId: threadId,
      inputMessageId: inputMessageId,
      status: status,
      assistantMessageId: assistantMessageId,
      failureKind: failureKind,
      failureRetryable: failureRetryable,
      retryOfRunId: retryOfRunId,
      clientRetryId: clientRetryId,
    );
  });
}

void _validateRunWriteStatus(int statusCode, _WireRun run) {
  final valid =
      (statusCode == HttpStatus.created && run.isTerminal) ||
      (statusCode == HttpStatus.accepted && !run.isTerminal);
  if (!valid) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidResponse,
      retryable: true,
    );
  }
}

T _decodeBody<T>(String body, T Function(Object? value) decode) {
  try {
    return decode(jsonDecode(body));
  } catch (_) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidResponse,
      retryable: true,
    );
  }
}

Map<String, Object?> _strictObject(
  Object? value, {
  required Set<String> allowed,
  required Set<String> required,
}) {
  if (value is! Map) {
    throw const _InvalidAgentResponse();
  }
  final result = <String, Object?>{};
  for (final entry in value.entries) {
    final key = entry.key;
    if (key is! String || !allowed.contains(key) || result.containsKey(key)) {
      throw const _InvalidAgentResponse();
    }
    result[key] = entry.value;
  }
  if (!result.keys.toSet().containsAll(required)) {
    throw const _InvalidAgentResponse();
  }
  return result;
}

List<Object?> _strictList(Object? value, {required int maxLength}) {
  if (value is! List || value.length > maxLength) {
    throw const _InvalidAgentResponse();
  }
  return List<Object?>.of(value);
}

String _strictString(
  Object? value, {
  required int minLength,
  required int maxLength,
}) {
  if (value is! String ||
      value.length < minLength ||
      value.length > maxLength) {
    throw const _InvalidAgentResponse();
  }
  return value;
}

String _strictPatternString(
  Object? value, {
  required RegExp pattern,
  required int maxLength,
}) {
  final result = _strictString(value, minLength: 1, maxLength: maxLength);
  if (!pattern.hasMatch(result)) {
    throw const _InvalidAgentResponse();
  }
  return result;
}

String _strictUuid(Object? value) {
  return _strictPatternString(value, pattern: _uuidPattern, maxLength: 36);
}

String _strictClientIdentity(Object? value) {
  return _strictPatternString(
    value,
    pattern: _clientIdentityPattern,
    maxLength: 128,
  );
}

int _strictInt(Object? value, {required int minimum}) {
  if (value is! int || value < minimum) {
    throw const _InvalidAgentResponse();
  }
  return value;
}

bool _strictBool(Object? value) {
  if (value is! bool) {
    throw const _InvalidAgentResponse();
  }
  return value;
}

DateTime _strictDateTime(Object? value) {
  final raw = _strictString(value, minLength: 1, maxLength: 64);
  final parsed = DateTime.tryParse(raw);
  if (parsed == null || !raw.contains(RegExp(r'(Z|[+-]\d{2}:\d{2})$'))) {
    throw const _InvalidAgentResponse();
  }
  return parsed.toUtc();
}

void _requireUuid(String value) {
  if (!_uuidPattern.hasMatch(value)) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidRequest,
    );
  }
}

void _requireClientIdentity(String value) {
  if (!_clientIdentityPattern.hasMatch(value) || value.length > 128) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidRequest,
    );
  }
}

void _requireContent(String value) {
  if (value.trim().isEmpty ||
      value.length > 4096 ||
      utf8.encode(value).length > 16384) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidRequest,
    );
  }
}

final RegExp _uuidPattern = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);
final RegExp _clientIdentityPattern = RegExp(
  r'^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$',
);
final RegExp _providerPattern = RegExp(r'^[a-z][a-z0-9_-]{0,63}$');
final RegExp _failureKindPattern = RegExp(r'^[a-z][a-z0-9_]{0,63}$');
const Set<String> _knownRunFailureKinds = <String>{
  'interrupted',
  'invalid_context',
  'internal_error',
  'invalid_request',
  'configuration',
  'authentication',
  'authorization',
  'quota_exhausted',
  'rate_limited',
  'timeout',
  'provider_unavailable',
  'invalid_response',
  'cancelled',
};

final class _InvalidAgentResponse implements Exception {
  const _InvalidAgentResponse();
}

final class _IoAgentHttpTransport implements IdentityHttpTransport {
  _IoAgentHttpTransport({required Duration requestTimeout})
    : _requestTimeout = requestTimeout,
      _httpClient = HttpClient()..connectionTimeout = requestTimeout;

  final Duration _requestTimeout;
  final HttpClient _httpClient;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    HttpClientRequest? request;
    try {
      request = await _httpClient.openUrl(method, uri).timeout(_requestTimeout);
      request.followRedirects = false;
      headers.forEach(request.headers.set);
      if (body != null) {
        request.write(body);
      }
      final response = await request.close().timeout(
        _requestTimeout,
        onTimeout: () {
          request?.abort();
          throw TimeoutException('Agent HTTP response timed out.');
        },
      );
      if (response.contentLength > _maxAgentResponseBytes) {
        request.abort();
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidResponse,
          retryable: true,
        );
      }
      final responseBytes = await _readBoundedResponse(response, request)
          .timeout(
            _requestTimeout,
            onTimeout: () {
              request?.abort();
              throw TimeoutException('Agent HTTP body timed out.');
            },
          );
      late final String responseBody;
      try {
        responseBody = utf8.decode(responseBytes);
      } on FormatException {
        request.abort();
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidResponse,
          retryable: true,
        );
      }
      final responseHeaders = <String, String>{};
      response.headers.forEach((name, values) {
        responseHeaders[name] = values.join(',');
      });
      return IdentityHttpResponse(
        statusCode: response.statusCode,
        body: responseBody,
        headers: responseHeaders,
      );
    } on TimeoutException {
      request?.abort();
      rethrow;
    }
  }

  void close({bool force = false}) {
    _httpClient.close(force: force);
  }

  Future<Uint8List> _readBoundedResponse(
    HttpClientResponse response,
    HttpClientRequest request,
  ) async {
    final builder = BytesBuilder(copy: false);
    var length = 0;
    await for (final chunk in response) {
      length += chunk.length;
      if (length > _maxAgentResponseBytes) {
        request.abort();
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidResponse,
          retryable: true,
        );
      }
      builder.add(chunk);
    }
    return builder.takeBytes();
  }
}

const _maxAgentResponseBytes = 1024 * 1024;
