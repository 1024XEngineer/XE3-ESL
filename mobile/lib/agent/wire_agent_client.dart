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

final class WireAgentClient
    implements
        AgentClient,
        AgentThreadHistoryClient,
        AgentPracticeAvailability {
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
  _AmbiguousThreadCreation? _ambiguousThreadCreation;
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
  Future<AgentThreadSnapshot> restoreThread() {
    return _runAccountOperation((generation) async {
      final page = await _fetchThreadPage(generation: generation);
      _requireCurrentGeneration(generation);
      final thread = _ambiguousThreadCreation != null
          ? await _recoverAmbiguousThreadCreation(generation, page: page)
          : page.threads.isEmpty
          ? await _createWireThreadSafely(generation, baseline: page)
          : page.threads.first;
      return _hydrateThread(generation: generation, thread: thread);
    });
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
  Future<AgentThreadSnapshot?> getFocusedThread() {
    return _runAccountOperation((generation) async {
      final response = await _send(
        generation: generation,
        method: 'GET',
        path: '/v1/agent-threads/focused',
      );
      _requireStatus(response, const <int>{
        HttpStatus.ok,
        HttpStatus.noContent,
      });
      if (response.statusCode == HttpStatus.noContent) {
        _requireEmptyBody(response);
        return null;
      }
      return _hydrateThread(
        generation: generation,
        thread: _decodeThread(response.body),
      );
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
  Future<AgentThreadSnapshot> setFocusedThread({required String threadId}) {
    return _runAccountOperation((generation) async {
      _requireUuid(threadId);
      final response = await _send(
        generation: generation,
        method: 'PUT',
        path: '/v1/agent-threads/focused',
        body: <String, Object?>{'thread_id': threadId},
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
  Future<void> clearFocusedThread() {
    return _runAccountOperation((generation) async {
      final response = await _send(
        generation: generation,
        method: 'DELETE',
        path: '/v1/agent-threads/focused',
      );
      _requireStatus(response, const <int>{HttpStatus.noContent});
      _requireEmptyBody(response);
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
    final activeMatter = thread.activeMatterId == null
        ? null
        : await _loadMatter(
            generation: generation,
            matterId: thread.activeMatterId!,
          );
    final messagePage = await _fetchMessagePage(
      generation: generation,
      threadId: thread.id,
    );
    _requireCurrentGeneration(generation);
    final recovery = await _restoreLastTextRun(
      generation: generation,
      threadId: thread.id,
      messages: messagePage.messages,
    );
    return AgentThreadSnapshot(
      threadId: thread.id,
      activeMatter: activeMatter,
      textRecovery: recovery.failure,
      messages: <AgentMessage>[
        for (final message in messagePage.messages) message.presentation,
        ?recovery.completedAssistant,
      ],
      createdAt: thread.createdAt,
      updatedAt: thread.updatedAt,
      nextMessageCursor: messagePage.nextCursor,
    );
  }

  Future<_RestoredTextRun> _restoreLastTextRun({
    required int generation,
    required String threadId,
    required List<_WireMessage> messages,
  }) async {
    if (messages.isEmpty || messages.last.role != AgentMessageRole.user) {
      return const _RestoredTextRun();
    }
    final userMessage = messages.last;
    final clientMessageId = userMessage.clientMessageId;
    if (clientMessageId == null) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    final operationKey = '$threadId\u{0}$clientMessageId';
    final terminalRun = await _pollUntilTerminal(
      generation: generation,
      initialRun: await _submitRun(
        generation: generation,
        threadId: threadId,
        text: userMessage.content,
        clientMessageId: clientMessageId,
      ),
    );
    _requireCurrentGeneration(generation);
    if (terminalRun.inputMessageId != userMessage.id) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    if (terminalRun.status == _WireRunStatus.completed) {
      _failedRuns.remove(operationKey);
      final exchange = await _loadCompletedExchange(
        generation: generation,
        run: terminalRun,
        expectedUserContent: userMessage.content,
        expectedClientMessageId: clientMessageId,
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

    final failureKind = terminalRun.failureKind;
    if (failureKind == null) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    if (terminalRun.failureRetryable) {
      _failedRuns[operationKey] = _FailedRun(
        runId: terminalRun.id,
        threadId: threadId,
        inputMessageId: terminalRun.inputMessageId,
        content: userMessage.content,
      );
    } else {
      _failedRuns.remove(operationKey);
    }
    return _RestoredTextRun(
      failure: AgentTextRecovery(
        text: userMessage.content,
        clientMessageId: clientMessageId,
        failureKind: failureKind,
        retryable: terminalRun.failureRetryable,
      ),
    );
  }

  Future<_WireThread> _createWireThread(int generation) async {
    final response = await _send(
      generation: generation,
      method: 'POST',
      path: '/v1/agent-threads',
      body: const <String, Object?>{},
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

  Future<AgentMatter> _loadMatter({
    required int generation,
    required String matterId,
  }) async {
    final response = await _send(
      generation: generation,
      method: 'GET',
      path: '/v1/matters/$matterId',
    );
    _requireStatus(response, const <int>{HttpStatus.ok});
    final matter = _decodeMatter(response.body);
    if (matter.id != matterId) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
        retryable: true,
      );
    }
    final knownScene = agentScenes
        .where((scene) => scene.title == matter.title)
        .firstOrNull;
    return AgentMatter(
      id: matter.id,
      scene:
          knownScene ??
          AgentScene(
            id: 'matter-${matter.id}',
            title: matter.title,
            description: '恢复的练习场景',
          ),
      status: matter.status,
      version: matter.version,
      createdAt: matter.createdAt,
      updatedAt: matter.updatedAt,
    );
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
      return _loadCompletedExchange(
        generation: generation,
        run: resolvedRun,
        expectedUserContent: text,
        expectedClientMessageId: clientMessageId,
      );
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
    required String expectedUserContent,
    required String expectedClientMessageId,
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
      _WireMessage? userMessage;
      _WireMessage? assistantMessage;
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

  Future<_WireMessagePage> _fetchMessagePage({
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

  @override
  Future<AgentSceneStart> startScene({
    required String threadId,
    required AgentScene scene,
    required String clientOperationId,
  }) {
    return _runAccountOperation((generation) async {
      _requireUuid(threadId);
      _requireClientIdentity(clientOperationId);
      _requireContent(scene.title);
      final listResponse = await _send(
        generation: generation,
        method: 'GET',
        path: '/v1/matters',
      );
      _requireStatus(listResponse, const <int>{HttpStatus.ok});
      final matters = _decodeMatterList(listResponse.body);
      var matter = matters
          .where((item) => item.title == scene.title && item.status == 'active')
          .firstOrNull;
      if (matter == null) {
        final createResponse = await _send(
          generation: generation,
          method: 'POST',
          path: '/v1/matters',
          body: <String, Object?>{'title': scene.title},
        );
        _requireStatus(createResponse, const <int>{HttpStatus.created});
        matter = _decodeMatter(createResponse.body);
      }
      final linkResponse = await _send(
        generation: generation,
        method: 'PUT',
        path: '/v1/agent-threads/$threadId/active-matter',
        body: <String, Object?>{'matter_id': matter.id},
      );
      _requireStatus(linkResponse, const <int>{HttpStatus.ok});
      _decodeMatterLink(
        linkResponse.body,
        expectedThreadId: threadId,
        expectedMatterId: matter.id,
      );
      return AgentSceneStart(
        activeMatter: AgentMatter(
          id: matter.id,
          scene: scene,
          status: matter.status,
          version: matter.version,
          createdAt: matter.createdAt,
          updatedAt: matter.updatedAt,
        ),
        assistantMessage: AgentMessage(
          id: 'scene-$clientOperationId',
          role: AgentMessageRole.assistant,
          text: scene.title,
        ),
      );
    });
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

const AgentClientException _practiceUnavailable = AgentClientException(
  kind: AgentClientFailureKind.unavailable,
);

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

final class _WireMatter {
  const _WireMatter({
    required this.id,
    required this.title,
    required this.status,
    required this.version,
    required this.createdAt,
    required this.updatedAt,
  });

  final String id;
  final String title;
  final String status;
  final int version;
  final DateTime createdAt;
  final DateTime updatedAt;
}

final class _WireThread {
  const _WireThread({
    required this.id,
    required this.createdAt,
    required this.updatedAt,
    this.activeMatterId,
  });

  final String id;
  final String? activeMatterId;
  final DateTime createdAt;
  final DateTime updatedAt;

  AgentThreadSummary get presentation => AgentThreadSummary(
    id: id,
    activeMatterId: activeMatterId,
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

final class _WireThreadPage {
  const _WireThreadPage({
    required this.threads,
    this.focusedThreadId,
    this.nextCursor,
  });

  final List<_WireThread> threads;
  final String? focusedThreadId;
  final String? nextCursor;

  AgentThreadPage get presentation => AgentThreadPage(
    threads: List<AgentThreadSummary>.unmodifiable(
      threads.map((thread) => thread.presentation),
    ),
    focusedThreadId: focusedThreadId,
    nextCursor: nextCursor,
  );
}

final class _WireMessage {
  const _WireMessage({
    required this.id,
    required this.role,
    required this.content,
    required this.sequence,
    required this.createdAt,
    required this.modality,
    this.clientMessageId,
    this.producedByRunId,
    this.audio,
  });

  final String id;
  final AgentMessageRole role;
  final String content;
  final int sequence;
  final DateTime createdAt;
  final AgentMessageModality modality;
  final String? clientMessageId;
  final String? producedByRunId;
  final AgentMessageAudio? audio;

  AgentMessage get presentation => AgentMessage(
    id: id,
    role: role,
    text: content,
    sequence: sequence,
    createdAt: createdAt,
    modality: modality,
    audio: audio,
  );
}

final class _WireMessagePage {
  const _WireMessagePage({required this.messages, this.nextCursor});

  final List<_WireMessage> messages;
  final String? nextCursor;

  AgentMessagePage get presentation => AgentMessagePage(
    messages: List<AgentMessage>.unmodifiable(
      messages.map((message) => message.presentation),
    ),
    nextCursor: nextCursor,
  );
}

final class _RestoredTextRun {
  const _RestoredTextRun({this.failure, this.completedAssistant});

  final AgentTextRecovery? failure;
  final AgentMessage? completedAssistant;
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

List<_WireMatter> _decodeMatterList(String body) {
  return _decodeBody(body, (value) {
    final root = _strictObject(
      value,
      allowed: const <String>{'matters'},
      required: const <String>{'matters'},
    );
    return [
      for (final item in _strictList(root['matters'], maxLength: 1000))
        _decodeMatterObject(item),
    ];
  });
}

_WireMatter _decodeMatter(String body) {
  return _decodeBody(body, _decodeMatterObject);
}

_WireMatter _decodeMatterObject(Object? value) {
  final object = _strictObject(
    value,
    allowed: const <String>{
      'matter_id',
      'title',
      'status',
      'version',
      'created_at',
      'updated_at',
    },
    required: const <String>{
      'matter_id',
      'title',
      'status',
      'version',
      'created_at',
      'updated_at',
    },
  );
  final version = _strictInt(object['version'], minimum: 1);
  final createdAt = _strictDateTime(object['created_at']);
  final updatedAt = _strictDateTime(object['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const _InvalidAgentResponse();
  }
  return _WireMatter(
    id: _strictUuid(object['matter_id']),
    title: _strictString(object['title'], minLength: 1, maxLength: 256),
    status: _strictString(object['status'], minLength: 1, maxLength: 32),
    version: version,
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

void _decodeMatterLink(
  String body, {
  required String expectedThreadId,
  required String expectedMatterId,
}) {
  _decodeBody(body, (value) {
    final object = _strictObject(
      value,
      allowed: const <String>{
        'thread_id',
        'matter_id',
        'active',
        'linked_at',
        'updated_at',
      },
      required: const <String>{
        'thread_id',
        'matter_id',
        'active',
        'linked_at',
        'updated_at',
      },
    );
    if (_strictUuid(object['thread_id']) != expectedThreadId ||
        _strictUuid(object['matter_id']) != expectedMatterId ||
        !_strictBool(object['active'])) {
      throw const _InvalidAgentResponse();
    }
    _strictDateTime(object['linked_at']);
    _strictDateTime(object['updated_at']);
    return true;
  });
}

_WireThreadPage _decodeThreadPage(String body) {
  return _decodeBody(body, (value) {
    final root = _strictObject(
      value,
      allowed: const <String>{'threads', 'focused_thread_id', 'next_cursor'},
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
      focusedThreadId: _absentOnlyOptional(
        root,
        'focused_thread_id',
        _strictUuid,
      ),
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
  final activeMatterId = _absentOnlyOptional(
    object,
    'active_matter_id',
    _strictUuid,
  );
  final createdAt = _strictDateTime(object['created_at']);
  final updatedAt = _strictDateTime(object['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const _InvalidAgentResponse();
  }
  return _WireThread(
    id: id,
    activeMatterId: activeMatterId,
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

_WireMessagePage _decodeMessagePage(
  String body, {
  required String expectedThreadId,
}) {
  return _decodeBody(body, (value) {
    final root = _strictObject(
      value,
      allowed: const <String>{'messages', 'next_cursor'},
      required: const <String>{'messages'},
    );
    final values = _strictList(root['messages'], maxLength: 100);
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
    return _WireMessagePage(
      messages: result,
      nextCursor: _absentOnlyOptional(
        root,
        'next_cursor',
        (value) => _strictString(value, minLength: 1, maxLength: 1024),
      ),
    );
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
      'modality',
      'content',
      'audio',
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
  final createdAt = _strictDateTime(object['created_at']);
  final modality = _absentOnlyOptional(
    object,
    'modality',
    (value) => switch (_strictString(value, minLength: 1, maxLength: 16)) {
      'voice' => AgentMessageModality.voice,
      _ => throw const _InvalidAgentResponse(),
    },
  );
  final audio = _absentOnlyOptional(object, 'audio', _decodeMessageAudio);
  final effectiveModality = modality ?? AgentMessageModality.text;
  final clientMessageId = _absentOnlyOptional(
    object,
    'client_message_id',
    _strictClientIdentity,
  );
  final producedByRunId = _absentOnlyOptional(
    object,
    'produced_by_run_id',
    _strictUuid,
  );
  if ((role == AgentMessageRole.user &&
          (clientMessageId == null || producedByRunId != null)) ||
      (role == AgentMessageRole.assistant &&
          (clientMessageId != null || producedByRunId == null)) ||
      (effectiveModality == AgentMessageModality.voice && audio == null) ||
      (effectiveModality == AgentMessageModality.text && audio != null) ||
      (effectiveModality == AgentMessageModality.voice &&
          role != AgentMessageRole.user)) {
    throw const _InvalidAgentResponse();
  }
  return _WireMessage(
    id: id,
    role: role,
    content: content,
    sequence: sequence,
    createdAt: createdAt,
    modality: effectiveModality,
    clientMessageId: clientMessageId,
    producedByRunId: producedByRunId,
    audio: audio,
  );
}

AgentMessageAudio _decodeMessageAudio(Object? value) {
  final object = _strictObject(
    value,
    allowed: const <String>{
      'audio_id',
      'status',
      'content_type',
      'size_bytes',
      'duration_ms',
      'playback_path',
      'deleted_at',
    },
    required: const <String>{
      'audio_id',
      'status',
      'content_type',
      'size_bytes',
      'duration_ms',
    },
  );
  final id = _strictUuid(object['audio_id']);
  final status = switch (_strictString(
    object['status'],
    minLength: 1,
    maxLength: 16,
  )) {
    'readable' => AgentMessageAudioStatus.readable,
    'deleting' => AgentMessageAudioStatus.deleting,
    'deleted' => AgentMessageAudioStatus.deleted,
    _ => throw const _InvalidAgentResponse(),
  };
  if (_strictString(object['content_type'], minLength: 1, maxLength: 32) !=
      'audio/wav') {
    throw const _InvalidAgentResponse();
  }
  final sizeBytes = _strictInt(
    object['size_bytes'],
    minimum: 1,
    maximum: 7400000,
  );
  final durationMs = _strictInt(
    object['duration_ms'],
    minimum: 1,
    maximum: 60000,
  );
  final playbackPath = _absentOnlyOptional(
    object,
    'playback_path',
    (value) => _strictPatternString(
      value,
      pattern: _agentMessageAudioPlaybackPathPattern,
      maxLength: 256,
    ),
  );
  final deletedAt = _absentOnlyOptional(object, 'deleted_at', _strictDateTime);
  if ((status == AgentMessageAudioStatus.readable &&
          (playbackPath == null || deletedAt != null)) ||
      (status == AgentMessageAudioStatus.deleting && playbackPath != null) ||
      (status == AgentMessageAudioStatus.deleted &&
          (playbackPath != null || deletedAt == null))) {
    throw const _InvalidAgentResponse();
  }
  return AgentMessageAudio(
    id: id,
    status: status,
    contentType: 'audio/wav',
    sizeBytes: sizeBytes,
    duration: Duration(milliseconds: durationMs),
    playbackPath: playbackPath,
    deletedAt: deletedAt,
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

String _strictClientIdentity(Object? value) {
  return _strictPatternString(
    value,
    pattern: _clientIdentityPattern,
    maxLength: 128,
  );
}

int _strictInt(Object? value, {required int minimum, int? maximum}) {
  if (value is! int ||
      value < minimum ||
      (maximum != null && value > maximum)) {
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

final RegExp _uuidPattern = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);
final RegExp _clientIdentityPattern = RegExp(
  r'^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$',
);
final RegExp _providerPattern = RegExp(r'^[a-z][a-z0-9_-]{0,63}$');
final RegExp _failureKindPattern = RegExp(r'^[a-z][a-z0-9_]{0,63}$');
final RegExp _agentMessageAudioPlaybackPathPattern = RegExp(
  r'^/v1/agent-message-audios/[0-9a-f-]+/playback$',
);
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
