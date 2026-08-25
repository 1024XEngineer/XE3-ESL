import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/bearer_authentication.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

import 'agent_wire_codec.dart';

final class WireAgentClient
    implements
        AgentClient,
        AgentStreamingTextClient,
        AgentMessageTranslationClient {
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
  final Map<String, AgentRun> _restoredRuns = <String, AgentRun>{};
  final Set<String> _ambiguousSubmissions = <String>{};
  _AmbiguousThreadCreation? _ambiguousThreadCreation;
  Future<void>? _cleanupFuture;

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
    _restoredRuns.clear();
    _ambiguousSubmissions.clear();
    _ambiguousThreadCreation = null;
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
  Future<AgentThreadPage> listThreads({int pageSize = 20, String? cursor}) {
    return _runAccountOperation((generation) async {
      _requirePageArguments(pageSize: pageSize, cursor: cursor);
      final page = await _fetchThreadPage(
        generation: generation,
        pageSize: pageSize,
        cursor: cursor,
      );
      return page.presentation;
    });
  }

  @override
  Future<AgentThreadSnapshot> getThread({required String threadId}) {
    return _runAccountOperation((generation) async {
      _requireUuid(threadId);
      final response = await _send(
        generation: generation,
        method: 'GET',
        path: '/v1/agent-threads/$threadId',
      );
      _requireStatus(response, const <int>{HttpStatus.ok});
      final thread = _decodeThread(response.body);
      if (thread.id != threadId) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidResponse,
          retryable: true,
        );
      }
      return _hydrateThread(generation: generation, thread: thread);
    });
  }

  @override
  Future<AgentThreadSummary> createThread() {
    return _runAccountOperation((generation) async {
      final thread = await _createWireThreadSafely(generation);
      return thread.presentation;
    });
  }

  @override
  Future<void> deleteThread({required String threadId}) {
    return _runAccountOperation((generation) async {
      _requireUuid(threadId);
      final response = await _send(
        generation: generation,
        method: 'DELETE',
        path: '/v1/agent-threads/$threadId',
      );
      _requireStatus(response, const <int>{HttpStatus.noContent});
      _requireEmptyBody(response);
    });
  }

  @override
  Future<AgentMessageTranslation> translateMessage({
    required String messageId,
  }) {
    return _runAccountOperation((generation) async {
      _requireUuid(messageId);
      final response = await _send(
        generation: generation,
        method: 'GET',
        path: '/v1/agent-messages/$messageId/translation',
      );
      _requireStatus(response, const <int>{HttpStatus.ok});
      return _decodeMessageTranslation(
        response.body,
        expectedMessageId: messageId,
      );
    });
  }

  @override
  Future<AgentMessagePage> listMessages({
    required String threadId,
    int pageSize = 50,
    String? cursor,
  }) {
    return _runAccountOperation((generation) async {
      _requireUuid(threadId);
      _requirePageArguments(pageSize: pageSize, cursor: cursor);
      final page = await _fetchMessagePage(
        generation: generation,
        threadId: threadId,
        pageSize: pageSize,
        cursor: cursor,
      );
      return page.presentation;
    });
  }

  Future<_WireThreadPage> _fetchThreadPage({
    required int generation,
    int? pageSize,
    String? cursor,
  }) async {
    final response = await _send(
      generation: generation,
      method: 'GET',
      path: '/v1/agent-threads',
      queryParameters: <String, String>{
        'page_size': ?pageSize?.toString(),
        'cursor': ?cursor,
      },
    );
    _requireStatus(response, const <int>{HttpStatus.ok});
    return _decodeThreadPage(response.body);
  }

  Future<AgentThreadSnapshot> _hydrateThread({
    required int generation,
    required _WireThread thread,
  }) async {
    final messagePage = await _fetchMessagePage(
      generation: generation,
      threadId: thread.id,
    );
    _requireCurrentGeneration(generation);
    final recovery = await _readLastTextRun(
      generation: generation,
      threadId: thread.id,
      messages: messagePage.messages,
    );
    return AgentThreadSnapshot(
      threadId: thread.id,
      title: thread.title,
      textRecovery: recovery.recovery,
      messages: <AgentMessage>[
        for (final message in messagePage.messages) message.presentation,
        ?recovery.completedAssistant,
      ],
      createdAt: thread.createdAt,
      updatedAt: thread.updatedAt,
      nextMessageCursor: messagePage.nextCursor,
    );
  }

  Future<_RestoredTextRun> _readLastTextRun({
    required int generation,
    required String threadId,
    required List<AgentWireMessage> messages,
  }) async {
    if (messages.isEmpty || messages.last.role != AgentMessageRole.user) {
      return const _RestoredTextRun();
    }
    final userMessage = messages.last;
    if (userMessage.modality == AgentMessageModality.voice) {
      return const _RestoredTextRun();
    }
    final imageAssetIds = <String>[
      for (final image in userMessage.images) image.id,
    ];
    final clientMessageId = userMessage.clientMessageId;
    if (clientMessageId == null) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    final operationKey = '$threadId\u{0}$clientMessageId';
    final latestRun = await _getLatestThreadRun(
      generation: generation,
      threadId: threadId,
    );
    _requireCurrentGeneration(generation);
    if (latestRun == null || latestRun.inputMessageId != userMessage.id) {
      _failedRuns.remove(operationKey);
      _restoredRuns.remove(operationKey);
      return _RestoredTextRun(
        recovery: AgentTextRecovery(
          text: userMessage.content,
          clientMessageId: clientMessageId,
          latestRun: null,
          imageAssetIds: imageAssetIds,
        ),
      );
    }
    if (latestRun.status == AgentRunStatus.completed) {
      _failedRuns.remove(operationKey);
      _restoredRuns.remove(operationKey);
      final exchange = await _loadCompletedExchange(
        generation: generation,
        run: latestRun,
        expectedUserContent: userMessage.content,
        expectedClientMessageId: clientMessageId,
        expectedImageAssetIds: imageAssetIds,
      );
      if (exchange.userMessage.id != userMessage.id ||
          exchange.assistantMessage == null) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidResponse,
          retryable: true,
        );
      }
      return _RestoredTextRun(completedAssistant: exchange.assistantMessage);
    }

    if (latestRun.status == AgentRunStatus.failed) {
      _restoredRuns.remove(operationKey);
      if (latestRun.failureRetryable) {
        _failedRuns[operationKey] = _FailedRun(
          run: latestRun,
          clientMessageId: clientMessageId,
          content: userMessage.content,
          imageAssetIds: imageAssetIds,
        );
      } else {
        _failedRuns.remove(operationKey);
      }
    } else {
      _failedRuns.remove(operationKey);
      _restoredRuns[operationKey] = latestRun;
    }
    return _RestoredTextRun(
      recovery: AgentTextRecovery(
        text: userMessage.content,
        clientMessageId: clientMessageId,
        latestRun: latestRun,
        imageAssetIds: imageAssetIds,
      ),
    );
  }

  Future<AgentRun?> _getLatestThreadRun({
    required int generation,
    required String threadId,
  }) async {
    final response = await _send(
      generation: generation,
      method: 'GET',
      path: '/v1/agent-threads/$threadId/runs/latest',
    );
    _requireStatus(response, const <int>{HttpStatus.ok});
    final run = _decodeLatestRun(response.body);
    if (run != null && run.threadId != threadId) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    return run;
  }

  Future<_WireThread> _createWireThread(int generation) async {
    final response = await _send(
      generation: generation,
      method: 'POST',
      path: '/v1/agent-threads',
    );
    _requireStatus(response, const <int>{HttpStatus.created});
    return _decodeThread(response.body);
  }

  Future<_WireThread> _createWireThreadSafely(
    int generation, {
    _WireThreadPage? baseline,
  }) async {
    if (_ambiguousThreadCreation != null) {
      return _recoverAmbiguousThreadCreation(generation);
    }
    final authoritativeBaseline =
        baseline ??
        await _fetchThreadPage(generation: generation, pageSize: 100);
    _requireCurrentGeneration(generation);
    final ambiguity = _AmbiguousThreadCreation(
      knownThreadIds: <String>{
        for (final thread in authoritativeBaseline.threads) thread.id,
      },
    );
    try {
      return await _createWireThread(generation);
    } on AgentClientException catch (error) {
      if (!_isAmbiguousThreadCreateFailure(error)) {
        rethrow;
      }
      _requireCurrentGeneration(generation);
      _ambiguousThreadCreation = ambiguity;
      return _recoverAmbiguousThreadCreation(generation);
    }
  }

  Future<_WireThread> _recoverAmbiguousThreadCreation(
    int generation, {
    _WireThreadPage? page,
  }) async {
    final ambiguity = _ambiguousThreadCreation;
    if (ambiguity == null) {
      throw const AgentClientException(kind: AgentClientFailureKind.unexpected);
    }
    try {
      final authoritativePage =
          page ?? await _fetchThreadPage(generation: generation, pageSize: 100);
      _requireCurrentGeneration(generation);
      final candidates = <_WireThread>[
        for (final thread in authoritativePage.threads)
          if (!ambiguity.knownThreadIds.contains(thread.id)) thread,
      ];
      if (candidates.length != 1) {
        throw const _UnresolvedThreadCreation();
      }
      _ambiguousThreadCreation = null;
      return candidates.single;
    } on AgentClientOperationCancelled {
      rethrow;
    } catch (_) {
      _requireCurrentGeneration(generation);
      throw _ambiguousThreadCreationFailure;
    }
  }

  bool _isAmbiguousThreadCreateFailure(AgentClientException error) {
    return error.kind == AgentClientFailureKind.network ||
        error.kind == AgentClientFailureKind.server ||
        error.kind == AgentClientFailureKind.invalidResponse ||
        error.kind == AgentClientFailureKind.unexpected;
  }

  @override
  Future<AgentExchange> sendText({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) {
    return _sendMessage(
      threadId: threadId,
      text: text,
      clientMessageId: clientMessageId,
      imageAssetIds: imageAssetIds,
    );
  }

  Future<AgentExchange> _sendMessage({
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) {
    return _runAccountOperation((generation) async {
      _requireUuid(threadId);
      _requireClientIdentity(clientMessageId);
      _requireContent(text);
      _requireImageAssetIds(imageAssetIds);
      final operationKey = '$threadId\u{0}$clientMessageId';
      final restoredRun = _restoredRuns[operationKey];
      var failedRun = _failedRuns[operationKey];
      if (restoredRun != null &&
          (restoredRun.threadId != threadId ||
              restoredRun.inputMessageId.trim().isEmpty)) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidResponse,
          retryable: true,
        );
      }
      if (failedRun != null &&
          (failedRun.threadId != threadId ||
              failedRun.clientMessageId != clientMessageId ||
              failedRun.content != text ||
              !_sameStrings(failedRun.imageAssetIds, imageAssetIds))) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.conflict,
          errorCode: 'idempotency_key_conflict',
        );
      }

      if (failedRun?.requiresReconciliation == true) {
        final reconciled = await _reconcileInterruptedRun(
          generation: generation,
          interrupted: failedRun!,
        );
        if (reconciled == null) {
          _failedRuns.remove(operationKey);
          failedRun = null;
        } else if (reconciled.status == AgentRunStatus.completed) {
          _failedRuns.remove(operationKey);
          return _loadCompletedExchange(
            generation: generation,
            run: reconciled,
            expectedUserContent: text,
            expectedClientMessageId: clientMessageId,
            expectedImageAssetIds: imageAssetIds,
          );
        } else if (!reconciled.failureRetryable) {
          _failedRuns.remove(operationKey);
          throw AgentClientException(
            kind: AgentClientFailureKind.runFailed,
            errorCode: reconciled.failureKind,
          );
        } else {
          failedRun = _FailedRun(
            run: reconciled,
            clientMessageId: clientMessageId,
            content: text,
            imageAssetIds: imageAssetIds,
          );
          _failedRuns[operationKey] = failedRun;
        }
      }

      late AgentRun initialRun;
      if (restoredRun != null) {
        initialRun = await _resumeRestoredRun(
          generation: generation,
          restoredRun: restoredRun,
          threadId: threadId,
          text: text,
          clientMessageId: clientMessageId,
          imageAssetIds: imageAssetIds,
        );
      } else if (failedRun != null) {
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
            imageAssetIds: imageAssetIds,
          );
        } on AgentClientException catch (error) {
          if (error.kind == AgentClientFailureKind.network) {
            _requireCurrentGeneration(generation);
            _ambiguousSubmissions.add(operationKey);
          }
          rethrow;
        }
        if (_ambiguousSubmissions.contains(operationKey)) {
          if (initialRun.status == AgentRunStatus.failed &&
              initialRun.failureRetryable) {
            _ambiguousSubmissions.remove(operationKey);
            final recoveredFailure = _FailedRun(
              run: initialRun,
              clientMessageId: clientMessageId,
              content: text,
              imageAssetIds: imageAssetIds,
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
          resolvedRun.status == AgentRunStatus.failed &&
          resolvedRun.failureRetryable) {
        _ambiguousSubmissions.remove(operationKey);
        final recoveredFailure = _FailedRun(
          run: resolvedRun,
          clientMessageId: clientMessageId,
          content: text,
          imageAssetIds: imageAssetIds,
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

      if (resolvedRun.status == AgentRunStatus.failed) {
        _restoredRuns.remove(operationKey);
        if (resolvedRun.failureRetryable) {
          _failedRuns[operationKey] = _FailedRun(
            run: resolvedRun,
            clientMessageId: clientMessageId,
            content: text,
            imageAssetIds: imageAssetIds,
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
      final exchange = await _loadCompletedExchange(
        generation: generation,
        run: resolvedRun,
        expectedUserContent: text,
        expectedClientMessageId: clientMessageId,
        expectedImageAssetIds: imageAssetIds,
      );
      _restoredRuns.remove(operationKey);
      return exchange;
    });
  }

  @override
  Stream<AgentTextStreamEvent> sendTextStream({
    required String threadId,
    required String text,
    required String clientMessageId,
  }) async* {
    final generation = _accountGeneration;
    _requireCurrentGeneration(generation);
    _requireUuid(threadId);
    _requireClientIdentity(clientMessageId);
    _requireContent(text);
    final operationKey = '$threadId\u{0}$clientMessageId';
    var failedRun = _failedRuns[operationKey];
    if (failedRun != null &&
        (failedRun.threadId != threadId ||
            failedRun.clientMessageId != clientMessageId ||
            failedRun.content != text)) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.conflict,
        errorCode: 'idempotency_key_conflict',
      );
    }
    if (failedRun?.requiresReconciliation == true) {
      final reconciled = await _reconcileInterruptedRun(
        generation: generation,
        interrupted: failedRun!,
      );
      if (reconciled == null) {
        _failedRuns.remove(operationKey);
        failedRun = null;
      } else if (reconciled.status == AgentRunStatus.completed) {
        _failedRuns.remove(operationKey);
        yield AgentRunCompleted(
          runId: reconciled.id,
          assistantMessageId: reconciled.assistantMessageId!,
          run: reconciled,
        );
        return;
      } else if (!reconciled.failureRetryable) {
        _failedRuns.remove(operationKey);
        yield AgentRunFailed(
          runId: reconciled.id,
          kind: reconciled.failureKind!,
          retryable: false,
          run: reconciled,
        );
        return;
      } else {
        failedRun = _FailedRun(
          run: reconciled,
          clientMessageId: clientMessageId,
          content: text,
        );
        _failedRuns[operationKey] = failedRun;
      }
    }
    final credential = _credentialProvider();
    if (credential == null) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
        errorCode: 'authentication_required',
      );
    }
    final uri = _baseUri.resolve(
      failedRun == null
          ? '/v1/agent-threads/$threadId/runs/stream'
          : '/v1/agent-runs/${failedRun.runId}/retries/stream',
    );
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(
      uri,
      sessionToken: credential.sessionToken,
    );
    final requestBody = utf8.encode(
      jsonEncode(
        failedRun == null
            ? <String, Object?>{
                'client_message_id': clientMessageId,
                'content': text,
              }
            : <String, Object?>{'client_retry_id': 'retry:${failedRun.runId}'},
      ),
    );
    AgentRun? committedRun;
    var reachedTerminal = false;

    void retainCommittedRunForReconciliation() {
      final run = committedRun;
      if (run == null || reachedTerminal || generation != _accountGeneration) {
        return;
      }
      _failedRuns[operationKey] = _FailedRun(
        run: run,
        clientMessageId: clientMessageId,
        content: text,
        requiresReconciliation: true,
      );
    }

    final httpClient = HttpClient()..connectionTimeout = _requestTimeout;
    try {
      final request = await httpClient.postUrl(uri).timeout(_requestTimeout);
      request
        ..followRedirects = false
        ..headers.set(HttpHeaders.acceptHeader, 'text/event-stream')
        ..headers.set(HttpHeaders.contentTypeHeader, ContentType.json.mimeType)
        ..headers.set(
          HttpHeaders.authorizationHeader,
          bearerAuthorizationValue(credential.sessionToken),
        )
        ..contentLength = requestBody.length
        ..add(requestBody);
      final response = await request.close().timeout(_requestTimeout);
      _requireCurrentGeneration(generation);
      if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
        throw const AgentClientOperationCancelled();
      }
      if (response.statusCode != HttpStatus.ok) {
        if (response.statusCode == HttpStatus.unauthorized) {
          unawaited(_invalidateCapturedCredential(credential));
        }
        await response.drain<void>();
        throw AgentClientException(
          kind: response.statusCode == HttpStatus.unauthorized
              ? AgentClientFailureKind.authenticationRequired
              : response.statusCode >= HttpStatus.internalServerError
              ? AgentClientFailureKind.server
              : AgentClientFailureKind.invalidRequest,
          statusCode: response.statusCode,
          retryable:
              response.statusCode >= HttpStatus.internalServerError ||
              response.statusCode == HttpStatus.tooManyRequests,
        );
      }
      final contentType = response.headers.contentType;
      if (contentType?.mimeType != 'text/event-stream') {
        await response.drain<void>();
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidResponse,
          retryable: true,
        );
      }
      String? eventName;
      String? outputId;
      final activeToolSteps = <String, String>{};
      final outputText = StringBuffer();
      var nextOutputSequence = 1;
      var eventCount = 0;
      var streamPhase = 0;
      await for (final line
          in response
              .transform(const Utf8Decoder())
              .transform(const LineSplitter())) {
        _requireCurrentGeneration(generation);
        if (++eventCount > 10000 || line.length > 1024 * 1024) {
          throw const AgentClientException(
            kind: AgentClientFailureKind.invalidResponse,
            retryable: true,
          );
        }
        if (line.isEmpty || line.startsWith(':')) {
          continue;
        }
        if (line.startsWith('event: ')) {
          if (eventName != null) {
            throw const AgentClientException(
              kind: AgentClientFailureKind.invalidResponse,
              retryable: true,
            );
          }
          eventName = line.substring(7);
          continue;
        }
        if (!line.startsWith('data: ') || eventName == null) {
          throw const AgentClientException(
            kind: AgentClientFailureKind.invalidResponse,
            retryable: true,
          );
        }
        final decoded = jsonDecode(line.substring(6));
        if (decoded is! Map<String, dynamic>) {
          throw const AgentClientException(
            kind: AgentClientFailureKind.invalidResponse,
            retryable: true,
          );
        }
        final decodedEvent = _decodeTextStreamEvent(
          eventName,
          decoded,
          expectedThreadId: threadId,
          expectedText: text,
          expectedClientMessageId: clientMessageId,
          failedRun: failedRun,
        );
        final event = decodedEvent.event;
        if (committedRun != null && event.runId != committedRun.id) {
          throw const AgentClientException(
            kind: AgentClientFailureKind.invalidResponse,
            retryable: true,
          );
        }
        switch (event) {
          case AgentRunCompleted(run: final terminalRun):
            if (committedRun == null ||
                terminalRun == null ||
                !sameAgentRunIdentity(terminalRun, committedRun)) {
              throw const AgentClientException(
                kind: AgentClientFailureKind.invalidResponse,
                retryable: true,
              );
            }
          case AgentRunFailed(run: final terminalRun) when terminalRun != null:
            if (committedRun == null ||
                !sameAgentRunIdentity(terminalRun, committedRun)) {
              throw const AgentClientException(
                kind: AgentClientFailureKind.invalidResponse,
                retryable: true,
              );
            }
          default:
            break;
        }
        switch (event) {
          case AgentInputCommitted() when streamPhase == 0:
            committedRun = decodedEvent.run;
            if (committedRun == null) {
              throw const AgentClientException(
                kind: AgentClientFailureKind.invalidResponse,
                retryable: true,
              );
            }
            streamPhase = 1;
          case AgentToolStepEvent(:final stepId, :final name, :final status)
              when streamPhase == 1:
            switch (status) {
              case AgentToolStepStatus.started:
                if (activeToolSteps.containsKey(stepId)) {
                  throw const AgentClientException(
                    kind: AgentClientFailureKind.invalidResponse,
                    retryable: true,
                  );
                }
                activeToolSteps[stepId] = name;
              case AgentToolStepStatus.completed:
              case AgentToolStepStatus.failed:
                if (activeToolSteps.remove(stepId) != name) {
                  throw const AgentClientException(
                    kind: AgentClientFailureKind.invalidResponse,
                    retryable: true,
                  );
                }
            }
          case AgentAssistantOutputStarted(outputId: final startedOutputId)
              when streamPhase == 1 && activeToolSteps.isEmpty:
            outputId = startedOutputId;
            streamPhase = 2;
          case AgentAssistantOutputDelta(
                outputId: final deltaOutputId,
                :final sequence,
                :final delta,
              )
              when streamPhase == 2 &&
                  deltaOutputId == outputId &&
                  sequence == nextOutputSequence:
            outputText.write(delta);
            nextOutputSequence++;
          case AgentAssistantOutputCompleted(
                outputId: final completedOutputId,
                :final text,
              )
              when streamPhase == 2 &&
                  completedOutputId == outputId &&
                  text == outputText.toString():
            streamPhase = 3;
          case AgentRunCompleted(:final assistantMessageId)
              when streamPhase == 3 && assistantMessageId == outputId:
            streamPhase = 4;
          case AgentRunCompleted(:final assistantMessageId)
              when streamPhase == 1 &&
                  activeToolSteps.isEmpty &&
                  outputId == null &&
                  committedRun!.status == AgentRunStatus.completed &&
                  assistantMessageId == committedRun.assistantMessageId:
            streamPhase = 4;
          case AgentRunFailed() when streamPhase >= 1 && streamPhase <= 3:
            streamPhase = 4;
          default:
            throw const AgentClientException(
              kind: AgentClientFailureKind.invalidResponse,
              retryable: true,
            );
        }
        reachedTerminal = streamPhase == 4;
        if (event case AgentRunFailed(:final retryable, :final run)) {
          if (retryable) {
            final failureRun = run ?? committedRun!;
            _failedRuns[operationKey] = _FailedRun(
              run: failureRun,
              clientMessageId: clientMessageId,
              content: text,
              requiresReconciliation: run == null,
            );
          } else {
            _failedRuns.remove(operationKey);
          }
        } else if (event is AgentRunCompleted) {
          _failedRuns.remove(operationKey);
        }
        yield event;
        eventName = null;
      }
      if (eventName != null) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidResponse,
          retryable: true,
        );
      }
      if (streamPhase != 4) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.network,
          retryable: true,
        );
      }
    } on AgentClientException {
      retainCommittedRunForReconciliation();
      rethrow;
    } on AgentClientOperationCancelled {
      rethrow;
    } on TimeoutException {
      retainCommittedRunForReconciliation();
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      retainCommittedRunForReconciliation();
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on AgentWireCodecException {
      retainCommittedRunForReconciliation();
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    } on _InvalidAgentResponse {
      retainCommittedRunForReconciliation();
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    } on FormatException {
      retainCommittedRunForReconciliation();
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    } finally {
      httpClient.close(force: true);
    }
  }

  _DecodedTextStreamEvent _decodeTextStreamEvent(
    String eventName,
    Map<String, dynamic> data, {
    required String expectedThreadId,
    required String expectedText,
    required String expectedClientMessageId,
    required _FailedRun? failedRun,
  }) {
    switch (eventName) {
      case 'input.committed':
        final object = _strictObject(
          data,
          allowed: const <String>{'run', 'message'},
          required: const <String>{'run', 'message'},
        );
        final run = decodeAgentWireRun(object['run']);
        final message = _decodeMessageObject(
          object['message'],
          expectedThreadId: expectedThreadId,
        );
        if (run.threadId != expectedThreadId ||
            run.inputMessageId != message.id ||
            message.clientMessageId != expectedClientMessageId ||
            message.content != expectedText) {
          throw const _InvalidAgentResponse();
        }
        if (failedRun == null) {
          if (run.attempt != 1 ||
              run.retryOfRunId != null ||
              run.clientRetryId != null) {
            throw const _InvalidAgentResponse();
          }
        } else {
          final expectedRetryId = 'retry:${failedRun.runId}';
          if (run.id == failedRun.runId ||
              run.threadId != failedRun.threadId ||
              run.inputMessageId != failedRun.inputMessageId ||
              run.attempt != failedRun.attempt + 1 ||
              run.retryOfRunId != failedRun.runId ||
              run.clientRetryId != expectedRetryId) {
            throw const _InvalidAgentResponse();
          }
        }
        return _DecodedTextStreamEvent(
          event: AgentInputCommitted(
            runId: run.id,
            userMessage: message.presentation,
          ),
          run: run,
        );
      case 'tool.started':
      case 'tool.completed':
      case 'tool.failed':
        final object = _strictObject(
          data,
          allowed: const <String>{'run_id', 'step_id', 'name'},
          required: const <String>{'run_id', 'step_id', 'name'},
        );
        return _DecodedTextStreamEvent(
          event: AgentToolStepEvent(
            runId: _strictUuid(object['run_id']),
            stepId: _strictPatternString(
              object['step_id'],
              pattern: _clientIdentityPattern,
              maxLength: 128,
            ),
            name: _strictPatternString(
              object['name'],
              pattern: _clientIdentityPattern,
              maxLength: 128,
            ),
            status: switch (eventName) {
              'tool.started' => AgentToolStepStatus.started,
              'tool.completed' => AgentToolStepStatus.completed,
              _ => AgentToolStepStatus.failed,
            },
          ),
        );
      case 'assistant.output.started':
        final object = _strictObject(
          data,
          allowed: const <String>{'run_id', 'output_id'},
          required: const <String>{'run_id', 'output_id'},
        );
        return _DecodedTextStreamEvent(
          event: AgentAssistantOutputStarted(
            runId: _strictUuid(object['run_id']),
            outputId: _strictUuid(object['output_id']),
          ),
        );
      case 'assistant.output.delta':
        final object = _strictObject(
          data,
          allowed: const <String>{'run_id', 'output_id', 'sequence', 'delta'},
          required: const <String>{'run_id', 'output_id', 'sequence', 'delta'},
        );
        return _DecodedTextStreamEvent(
          event: AgentAssistantOutputDelta(
            runId: _strictUuid(object['run_id']),
            outputId: _strictUuid(object['output_id']),
            sequence: _strictInt(
              object['sequence'],
              minimum: 1,
              maximum: 10000,
            ),
            delta: _strictDelta(object['delta']),
          ),
        );
      case 'assistant.output.completed':
        final object = _strictObject(
          data,
          allowed: const <String>{'run_id', 'output_id', 'text'},
          required: const <String>{'run_id', 'output_id', 'text'},
        );
        return _DecodedTextStreamEvent(
          event: AgentAssistantOutputCompleted(
            runId: _strictUuid(object['run_id']),
            outputId: _strictUuid(object['output_id']),
            text: _strictContent(object['text']),
          ),
        );
      case 'run.completed':
        final object = _strictObject(
          data,
          allowed: const <String>{'run'},
          required: const <String>{'run'},
        );
        final decodedRun = decodeAgentWireRun(object['run']);
        final assistantMessageId = decodedRun.assistantMessageId;
        if (decodedRun.threadId != expectedThreadId ||
            decodedRun.status != AgentRunStatus.completed ||
            assistantMessageId == null) {
          throw const _InvalidAgentResponse();
        }
        return _DecodedTextStreamEvent(
          event: AgentRunCompleted(
            runId: decodedRun.id,
            assistantMessageId: assistantMessageId,
            run: decodedRun,
          ),
          run: decodedRun,
        );
      case 'run.failed':
        if (data.containsKey('run')) {
          final object = _strictObject(
            data,
            allowed: const <String>{'run', 'kind', 'retryable'},
            required: const <String>{'run', 'kind', 'retryable'},
          );
          final decodedRun = decodeAgentWireRun(object['run']);
          final kind = _strictString(
            object['kind'],
            minLength: 1,
            maxLength: 64,
          );
          final retryable = _strictBool(object['retryable']);
          if (decodedRun.threadId != expectedThreadId ||
              decodedRun.status != AgentRunStatus.failed ||
              decodedRun.failureKind != kind ||
              decodedRun.failureRetryable != retryable) {
            throw const _InvalidAgentResponse();
          }
          return _DecodedTextStreamEvent(
            event: AgentRunFailed(
              runId: decodedRun.id,
              kind: kind,
              retryable: retryable,
              run: decodedRun,
            ),
            run: decodedRun,
          );
        }
        final object = _strictObject(
          data,
          allowed: const <String>{'run_id', 'kind', 'retryable'},
          required: const <String>{'run_id', 'kind', 'retryable'},
        );
        final runId = _strictUuid(object['run_id']);
        final kind = _strictString(object['kind'], minLength: 1, maxLength: 64);
        final retryable = _strictBool(object['retryable']);
        if (kind != 'stream_interrupted' || !retryable) {
          throw const _InvalidAgentResponse();
        }
        return _DecodedTextStreamEvent(
          event: AgentRunFailed(runId: runId, kind: kind, retryable: retryable),
        );
      default:
        throw const _InvalidAgentResponse();
    }
  }

  Future<AgentRun?> _reconcileInterruptedRun({
    required int generation,
    required _FailedRun interrupted,
  }) async {
    final response = await _send(
      generation: generation,
      method: 'GET',
      path: '/v1/agent-runs/${interrupted.runId}',
    );
    if (response.statusCode == HttpStatus.notFound) {
      return null;
    }
    _requireStatus(response, const <int>{HttpStatus.ok});
    final authoritative = _decodeRun(response.body);
    if (!sameAgentRunIdentity(authoritative, interrupted.run)) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    return _pollUntilTerminal(
      generation: generation,
      initialRun: authoritative,
    );
  }

  Future<AgentRun> _submitRun({
    required int generation,
    required String threadId,
    required String text,
    required String clientMessageId,
    List<String> imageAssetIds = const <String>[],
  }) async {
    final response = await _send(
      generation: generation,
      method: 'POST',
      path: '/v1/agent-threads/$threadId/runs',
      body: <String, Object?>{
        'client_message_id': clientMessageId,
        'content': text,
        if (imageAssetIds.isNotEmpty) 'image_asset_ids': imageAssetIds,
      },
    );
    _requireStatus(response, const <int>{
      HttpStatus.created,
      HttpStatus.accepted,
    });
    final run = _decodeRun(response.body);
    _validateRunWriteStatus(response.statusCode, run);
    if (run.threadId != threadId ||
        run.attempt != 1 ||
        run.retryOfRunId != null ||
        run.clientRetryId != null) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    return run;
  }

  Future<AgentRun> _resumeRestoredRun({
    required int generation,
    required AgentRun restoredRun,
    required String threadId,
    required String text,
    required String clientMessageId,
    required List<String> imageAssetIds,
  }) async {
    late final AgentRun current;
    if (restoredRun.attempt == 1) {
      current = await _submitRun(
        generation: generation,
        threadId: threadId,
        text: text,
        clientMessageId: clientMessageId,
        imageAssetIds: imageAssetIds,
      );
    } else {
      final sourceRunId = restoredRun.retryOfRunId;
      final retryClientId = restoredRun.clientRetryId;
      if (sourceRunId == null || retryClientId == null) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidResponse,
          retryable: true,
        );
      }
      final response = await _send(
        generation: generation,
        method: 'POST',
        path: '/v1/agent-runs/$sourceRunId/retries',
        body: <String, Object?>{'client_retry_id': retryClientId},
      );
      _requireStatus(response, const <int>{
        HttpStatus.created,
        HttpStatus.accepted,
      });
      current = _decodeRun(response.body);
      _validateRunWriteStatus(response.statusCode, current);
    }
    if (!sameAgentRunIdentity(current, restoredRun)) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    return current;
  }

  Future<AgentRun> _retryRun({
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
    if (run.id == failedRun.runId ||
        run.threadId != failedRun.threadId ||
        run.inputMessageId != failedRun.inputMessageId ||
        run.attempt != failedRun.attempt + 1 ||
        run.retryOfRunId != failedRun.runId ||
        run.clientRetryId != retryClientId) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    return run;
  }

  Future<AgentRun> _pollUntilTerminal({
    required int generation,
    required AgentRun initialRun,
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
      if (!sameAgentRunIdentity(restored, initialRun)) {
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
    required AgentRun run,
    required String expectedUserContent,
    required String expectedClientMessageId,
    List<String> expectedImageAssetIds = const <String>[],
  }) async {
    final assistantMessageId = run.assistantMessageId;
    if (assistantMessageId == null) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    for (var attempt = 0; attempt < _maxMessagePollAttempts; attempt++) {
      final messagePage = await _fetchMessagePage(
        generation: generation,
        threadId: run.threadId,
      );
      AgentWireMessage? userMessage;
      AgentWireMessage? assistantMessage;
      for (final message in messagePage.messages) {
        if (message.id == run.inputMessageId) {
          userMessage = message;
        }
        if (message.id == assistantMessageId) {
          assistantMessage = message;
        }
      }
      if (userMessage != null &&
          userMessage.role == AgentMessageRole.user &&
          userMessage.content == expectedUserContent &&
          userMessage.clientMessageId == expectedClientMessageId &&
          _sameStrings(<String>[
            for (final image in userMessage.images) image.id,
          ], expectedImageAssetIds) &&
          assistantMessage != null &&
          assistantMessage.role == AgentMessageRole.assistant &&
          assistantMessage.sequence > userMessage.sequence &&
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

  Future<AgentWireMessagePage> _fetchMessagePage({
    required int generation,
    required String threadId,
    int? pageSize,
    String? cursor,
  }) async {
    final response = await _send(
      generation: generation,
      method: 'GET',
      path: '/v1/agent-threads/$threadId/messages',
      queryParameters: <String, String>{
        'page_size': ?pageSize?.toString(),
        'cursor': ?cursor,
      },
    );
    _requireStatus(response, const <int>{HttpStatus.ok});
    return _decodeMessagePage(response.body, expectedThreadId: threadId);
  }

  Future<void> _waitForPoll(int generation) async {
    if (_pollInterval != Duration.zero) {
      await Future<void>.delayed(_pollInterval);
    }
    _requireCurrentGeneration(generation);
  }

  Future<IdentityHttpResponse> _send({
    required int generation,
    required String method,
    required String path,
    Map<String, String>? queryParameters,
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
    final resolvedUri = _baseUri.resolve(path);
    final uri = queryParameters == null || queryParameters.isEmpty
        ? resolvedUri
        : resolvedUri.replace(queryParameters: queryParameters);
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

  void _requireEmptyBody(IdentityHttpResponse response) {
    if (response.body.isNotEmpty) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
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

const AgentClientException _ambiguousThreadCreationFailure =
    AgentClientException(
      kind: AgentClientFailureKind.network,
      errorCode: 'thread_creation_ambiguous',
      retryable: true,
    );

final class _AmbiguousThreadCreation {
  const _AmbiguousThreadCreation({required this.knownThreadIds});

  final Set<String> knownThreadIds;
}

final class _UnresolvedThreadCreation implements Exception {
  const _UnresolvedThreadCreation();
}

final class _DecodedTextStreamEvent {
  const _DecodedTextStreamEvent({required this.event, this.run});

  final AgentTextStreamEvent event;
  final AgentRun? run;
}

final class _FailedRun {
  const _FailedRun({
    required this.run,
    required this.clientMessageId,
    required this.content,
    this.imageAssetIds = const <String>[],
    this.requiresReconciliation = false,
  });

  final AgentRun run;
  final String clientMessageId;
  final String content;
  final List<String> imageAssetIds;
  final bool requiresReconciliation;

  String get runId => run.id;
  String get threadId => run.threadId;
  String get inputMessageId => run.inputMessageId;
  int get attempt => run.attempt;
}

final class _WireThread {
  const _WireThread({
    required this.id,
    required this.createdAt,
    required this.updatedAt,
    this.title,
  });

  final String id;
  final String? title;
  final DateTime createdAt;
  final DateTime updatedAt;

  AgentThreadSummary get presentation => AgentThreadSummary(
    id: id,
    title: title,
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

final class _WireThreadPage {
  const _WireThreadPage({required this.threads, this.nextCursor});

  final List<_WireThread> threads;
  final String? nextCursor;

  AgentThreadPage get presentation => AgentThreadPage(
    threads: List<AgentThreadSummary>.unmodifiable(
      threads.map((thread) => thread.presentation),
    ),
    nextCursor: nextCursor,
  );
}

final class _RestoredTextRun {
  const _RestoredTextRun({this.recovery, this.completedAssistant});

  final AgentTextRecovery? recovery;
  final AgentMessage? completedAssistant;
}

_WireThreadPage _decodeThreadPage(String body) {
  return _decodeBody(body, (value) {
    final root = _strictObject(
      value,
      allowed: const <String>{'threads', 'next_cursor'},
      required: const <String>{'threads'},
    );
    final values = _strictList(root['threads'], maxLength: 100);
    final threads = <_WireThread>[];
    final threadIds = <String>{};
    _WireThread? previous;
    for (final value in values) {
      final thread = _decodeThreadObject(value);
      if (!threadIds.add(thread.id) ||
          (previous != null &&
              (thread.updatedAt.isAfter(previous.updatedAt) ||
                  (thread.updatedAt == previous.updatedAt &&
                      previous.id.compareTo(thread.id) <= 0)))) {
        throw const _InvalidAgentResponse();
      }
      threads.add(thread);
      previous = thread;
    }
    return _WireThreadPage(
      threads: threads,
      nextCursor: _absentOnlyOptional(
        root,
        'next_cursor',
        (value) => _strictString(value, minLength: 1, maxLength: 1024),
      ),
    );
  });
}

_WireThread _decodeThread(String body) {
  return _decodeBody(body, _decodeThreadObject);
}

AgentMessageTranslation _decodeMessageTranslation(
  String body, {
  required String expectedMessageId,
}) {
  return _decodeBody(body, (value) {
    final root = _strictObject(
      value,
      allowed: const <String>{'message_id', 'target_language', 'translation'},
      required: const <String>{'message_id', 'target_language', 'translation'},
    );
    final messageId = _strictUuid(root['message_id']);
    final targetLanguage = _strictString(
      root['target_language'],
      minLength: 5,
      maxLength: 5,
    );
    final content = _strictString(
      root['translation'],
      minLength: 1,
      maxLength: 8000,
    );
    if (messageId != expectedMessageId || targetLanguage != 'zh-CN') {
      throw const _InvalidAgentResponse();
    }
    return AgentMessageTranslation(
      messageId: messageId,
      targetLanguage: targetLanguage,
      content: content,
    );
  });
}

_WireThread _decodeThreadObject(Object? value) {
  final object = _strictObject(
    value,
    allowed: const <String>{'thread_id', 'title', 'created_at', 'updated_at'},
    required: const <String>{'thread_id', 'title', 'created_at', 'updated_at'},
  );
  final id = _strictUuid(object['thread_id']);
  final titleValue = object['title'];
  final title = titleValue == null
      ? null
      : _strictString(titleValue, minLength: 1, maxLength: 32);
  final createdAt = _strictDateTime(object['created_at']);
  final updatedAt = _strictDateTime(object['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const _InvalidAgentResponse();
  }
  return _WireThread(
    id: id,
    title: title,
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

AgentWireMessagePage _decodeMessagePage(
  String body, {
  required String expectedThreadId,
}) {
  return _decodeBody(
    body,
    (value) =>
        decodeAgentWireMessagePage(value, expectedThreadId: expectedThreadId),
  );
}

AgentWireMessage _decodeMessageObject(
  Object? value, {
  required String expectedThreadId,
}) {
  return decodeAgentWireMessage(value, expectedThreadId: expectedThreadId);
}

AgentRun _decodeRun(String body) {
  return _decodeBody(body, decodeAgentWireRun);
}

AgentRun? _decodeLatestRun(String body) {
  return _decodeBody(body, (value) {
    final object = _strictObject(
      value,
      allowed: const <String>{'run'},
      required: const <String>{},
    );
    return _absentOnlyOptional(object, 'run', decodeAgentWireRun);
  });
}

void _validateRunWriteStatus(int statusCode, AgentRun run) {
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

T? _absentOnlyOptional<T>(
  Map<String, Object?> object,
  String key,
  T Function(Object? value) decode,
) {
  if (!object.containsKey(key)) {
    return null;
  }
  final value = object[key];
  if (value == null) {
    throw const _InvalidAgentResponse();
  }
  return decode(value);
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
  if (value is! String) {
    throw const _InvalidAgentResponse();
  }
  final length = value.runes.length;
  if (length < minLength || length > maxLength) {
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

int _strictInt(Object? value, {required int minimum, int? maximum}) {
  if (value is! int ||
      value < minimum ||
      (maximum != null && value > maximum)) {
    throw const _InvalidAgentResponse();
  }
  return value;
}

String _strictContent(Object? value) {
  final content = _strictString(value, minLength: 1, maxLength: 4096);
  if (content.trim().isEmpty || utf8.encode(content).length > 16384) {
    throw const _InvalidAgentResponse();
  }
  return content;
}

String _strictDelta(Object? value) {
  final delta = _strictString(value, minLength: 1, maxLength: 4096);
  if (utf8.encode(delta).length > 16384) {
    throw const _InvalidAgentResponse();
  }
  return delta;
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

void _requirePageArguments({required int pageSize, required String? cursor}) {
  if (pageSize < 1 ||
      pageSize > 100 ||
      (cursor != null &&
          (cursor.runes.isEmpty || cursor.runes.length > 1024))) {
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
      value.runes.length > 4096 ||
      utf8.encode(value).length > 16384) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidRequest,
    );
  }
}

void _requireImageAssetIds(List<String> values) {
  if (values.length > agentMaximumImagesPerMessage ||
      values.toSet().length != values.length) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidRequest,
    );
  }
  for (final value in values) {
    _requireUuid(value);
  }
}

bool _sameStrings(List<String> left, List<String> right) {
  if (left.length != right.length) {
    return false;
  }
  for (var index = 0; index < left.length; index++) {
    if (left[index] != right[index]) {
      return false;
    }
  }
  return true;
}

final RegExp _uuidPattern = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);
final RegExp _clientIdentityPattern = RegExp(
  r'^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$',
);

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
    List<int>? bodyBytes,
  }) async {
    if (body != null && bodyBytes != null) {
      throw ArgumentError('Only one request body may be provided.');
    }
    HttpClientRequest? request;
    try {
      request = await _httpClient.openUrl(method, uri).timeout(_requestTimeout);
      request.followRedirects = false;
      headers.forEach(request.headers.set);
      if (bodyBytes != null) {
        request.add(bodyBytes);
      } else if (body != null) {
        request.add(utf8.encode(body));
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
