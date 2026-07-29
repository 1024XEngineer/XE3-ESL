import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/bearer_authentication.dart';
import 'package:speakup/identity/network/transport_security.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/practice/practice_models.dart';

/// The single replaceable location for the frozen #87 voice-practice routes.
///
/// UI and Controller code depend only on [PracticeClient].
final class PracticeWireEndpoints {
  const PracticeWireEndpoints({
    this.restoreByThread =
        '/v1/agent-threads/{thread_id}/voice-practice-session',
    this.startByThread =
        '/v1/agent-threads/{thread_id}/voice-practice-sessions',
    this.transcribe =
        '/v1/voice-practice-sessions/{practice_session_id}/questions/'
        '{question_id}/transcription-candidates',
    this.submitText =
        '/v1/voice-practice-sessions/{practice_session_id}/questions/'
        '{question_id}/text-answers',
    this.confirm = '/v1/transcription-candidates/{candidate_id}/confirmations',
    this.endEarly = '/v1/practice-sessions/{practice_session_id}/end-early',
  });

  final String restoreByThread;
  final String startByThread;
  final String transcribe;
  final String submitText;
  final String confirm;
  final String endEarly;

  String restorePath(String threadId) =>
      restoreByThread.replaceAll('{thread_id}', _pathSegment(threadId));

  String startPath(String threadId) =>
      startByThread.replaceAll('{thread_id}', _pathSegment(threadId));

  String transcribePath(String sessionId, String questionId) => transcribe
      .replaceAll('{practice_session_id}', _pathSegment(sessionId))
      .replaceAll('{question_id}', _pathSegment(questionId));

  String confirmPath(String candidateId) =>
      confirm.replaceAll('{candidate_id}', _pathSegment(candidateId));

  String submitTextPath(String sessionId, String questionId) => submitText
      .replaceAll('{practice_session_id}', _pathSegment(sessionId))
      .replaceAll('{question_id}', _pathSegment(questionId));

  String endEarlyPath(String sessionId) =>
      endEarly.replaceAll('{practice_session_id}', _pathSegment(sessionId));
}

String _pathSegment(String value) => Uri.encodeComponent(value);

final class PracticeWireRequest {
  const PracticeWireRequest({
    required this.method,
    required this.uri,
    required this.headers,
    this.jsonBody,
    this.rawFilePath,
  }) : assert(jsonBody == null || rawFilePath == null);

  final String method;
  final Uri uri;
  final Map<String, String> headers;
  final String? jsonBody;
  final String? rawFilePath;
}

final class PracticeWireResponse {
  const PracticeWireResponse({
    required this.statusCode,
    required this.body,
    this.headers = const <String, String>{},
  });

  final int statusCode;
  final String body;
  final Map<String, String> headers;
}

abstract interface class PracticeWireTransport {
  Future<PracticeWireResponse> send(PracticeWireRequest request);

  void close({bool force = false});
}

final class WirePracticeClient
    implements PracticeClient, PracticeLifecycleClient {
  factory WirePracticeClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    PracticeWireTransport? transport,
    PracticeWireTransport Function()? transportFactory,
    PracticeWireEndpoints endpoints = const PracticeWireEndpoints(),
    Duration jsonTimeout = const Duration(seconds: 30),
    Duration transcriptionTimeout = const Duration(seconds: 120),
  }) {
    if (jsonTimeout <= Duration.zero ||
        transcriptionTimeout <= Duration.zero ||
        (transport != null && transportFactory != null)) {
      throw ArgumentError('Practice request timeout must be positive.');
    }
    final createTransport = transportFactory ?? IoPracticeWireTransport.new;
    final ownsTransport = transport == null;
    return WirePracticeClient._(
      baseUri,
      TrustedIdentityHttpOrigin(baseUri),
      credentialProvider,
      invalidateSession,
      transport ?? createTransport(),
      ownsTransport,
      createTransport,
      endpoints,
      jsonTimeout,
      transcriptionTimeout,
    );
  }

  WirePracticeClient._(
    this._baseUri,
    this._trustedOrigin,
    this._credentialProvider,
    this._invalidateSession,
    this._transport,
    this._ownsTransport,
    this._transportFactory,
    this._endpoints,
    this._jsonTimeout,
    this._transcriptionTimeout,
  );

  static const _maximumAudioBytes = 2000044;

  final Uri _baseUri;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  final AuthSessionCredentialProvider _credentialProvider;
  final AuthSessionInvalidator _invalidateSession;
  PracticeWireTransport _transport;
  final bool _ownsTransport;
  final PracticeWireTransport Function() _transportFactory;
  final PracticeWireEndpoints _endpoints;
  final Duration _jsonTimeout;
  final Duration _transcriptionTimeout;

  int _accountGeneration = 0;
  final Set<Future<void>> _inFlight = <Future<void>>{};

  @override
  Future<void> clearAccountState() async {
    _accountGeneration++;
    if (_ownsTransport) {
      _transport.close(force: true);
    }
    await Future.wait(List<Future<void>>.of(_inFlight));
    if (_ownsTransport) {
      _transport = _transportFactory();
    }
  }

  @override
  Future<PracticeSessionSnapshot?> restorePractice({
    required String threadId,
    AgentMatter? activeMatter,
  }) {
    return _run((generation) async {
      _requireOpaqueId(threadId);
      final response = await _sendJson(
        generation: generation,
        method: 'GET',
        path: _endpoints.restorePath(threadId),
      );
      if (response.statusCode == HttpStatus.notFound) {
        return null;
      }
      _requireStatus(response, const {HttpStatus.ok});
      return _decodeSessionState(response.body, expectedThreadId: threadId);
    });
  }

  @override
  Future<PracticeStartResult> startPractice({
    required String threadId,
    required AgentMatter activeMatter,
    required String clientOperationId,
  }) {
    return _run((generation) async {
      _requireOpaqueId(threadId);
      _requireOpaqueId(activeMatter.id);
      _requireClientId(clientOperationId);
      final response = await _send(
        generation: generation,
        timeout: _jsonTimeout,
        method: 'POST',
        path: _endpoints.startPath(threadId),
        extraHeaders: <String, String>{'Idempotency-Key': clientOperationId},
      );
      _requireStatus(response, const {HttpStatus.created});
      return PracticeStartResult(
        snapshot: _decodeSessionState(
          response.body,
          expectedThreadId: threadId,
          expectedMatterId: activeMatter.id,
        ),
      );
    });
  }

  @override
  Future<TranscriptionCandidate> transcribe(
    PracticeTranscriptionRequest request,
  ) {
    return _run((generation) async {
      _requireOpaqueId(request.sessionId);
      _requireOpaqueId(request.questionId);
      _requireClientId(request.clientTurnId);
      final audio = request.audio;
      if (audio.contentType != 'audio/wav' ||
          audio.sizeBytes < 45 ||
          audio.sizeBytes > _maximumAudioBytes) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidRequest,
          errorCode: 'invalid_audio',
        );
      }
      final audioType = await FileSystemEntity.type(
        audio.path,
        followLinks: false,
      );
      final file = File(audio.path);
      if (audioType != FileSystemEntityType.file ||
          await file.length() != audio.sizeBytes) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidRequest,
          errorCode: 'invalid_audio',
        );
      }
      final response = await _send(
        generation: generation,
        timeout: _transcriptionTimeout,
        method: 'POST',
        path: _endpoints.transcribePath(request.sessionId, request.questionId),
        extraHeaders: <String, String>{
          'Idempotency-Key': request.clientTurnId,
          HttpHeaders.contentTypeHeader: 'audio/wav',
        },
        rawFilePath: audio.path,
      );
      _requireStatus(response, const {HttpStatus.created});
      return _decodeCandidate(
        response.body,
        expectedSessionId: request.sessionId,
        expectedQuestionId: request.questionId,
      );
    });
  }

  @override
  Future<PracticeTurnConfirmation> confirm({
    required String sessionId,
    required String questionId,
    required String candidateId,
    required String idempotencyKey,
  }) {
    return _run((generation) async {
      _requireOpaqueId(sessionId);
      _requireOpaqueId(questionId);
      _requireOpaqueId(candidateId);
      _requireClientId(idempotencyKey);
      final response = await _send(
        generation: generation,
        timeout: _jsonTimeout,
        method: 'POST',
        path: _endpoints.confirmPath(candidateId),
        extraHeaders: <String, String>{'Idempotency-Key': idempotencyKey},
      );
      _requireStatus(response, const {HttpStatus.ok});
      final state = _decodeSessionState(
        response.body,
        expectedSessionId: sessionId,
      );
      return _confirmationFromState(
        state,
        expectedQuestionId: questionId,
        expectedCandidateId: candidateId,
      );
    });
  }

  @override
  Future<PracticeTurnConfirmation> submitText({
    required String sessionId,
    required String questionId,
    required String answerText,
    required String idempotencyKey,
  }) {
    return _run((generation) async {
      _requireOpaqueId(sessionId);
      _requireOpaqueId(questionId);
      _requireClientId(idempotencyKey);
      final text = answerText.trim();
      if (text.isEmpty || text.length > 8000) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidRequest,
          errorCode: 'invalid_answer_text',
        );
      }
      final response = await _sendJson(
        generation: generation,
        method: 'POST',
        path: _endpoints.submitTextPath(sessionId, questionId),
        body: <String, Object?>{'answer_text': text},
        extraHeaders: <String, String>{'Idempotency-Key': idempotencyKey},
      );
      _requireStatus(response, const {HttpStatus.ok});
      final state = _decodeSessionState(
        response.body,
        expectedSessionId: sessionId,
      );
      final confirmation = _confirmationFromState(
        state,
        expectedQuestionId: questionId,
      );
      if (confirmation.answer.text != text) {
        throw _invalidResponse();
      }
      return confirmation;
    });
  }

  @override
  Future<PracticeSessionLifecycle> endEarly({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  }) {
    return _run((generation) async {
      _requireOpaqueId(sessionId);
      _requireClientId(idempotencyKey);
      if (expectedSessionVersion < 1) {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidRequest,
        );
      }
      final response = await _sendJson(
        generation: generation,
        method: 'POST',
        path: _endpoints.endEarlyPath(sessionId),
        body: <String, Object?>{
          'expected_session_version': expectedSessionVersion,
        },
        extraHeaders: <String, String>{'Idempotency-Key': idempotencyKey},
      );
      _requireStatus(response, const {HttpStatus.ok});
      final lifecycle = _decodeSessionLifecycle(
        response.body,
        expectedSessionId: sessionId,
      );
      if (lifecycle.status != PracticeSessionLifecycleStatus.endedEarly ||
          lifecycle.version <= expectedSessionVersion) {
        throw _invalidResponse();
      }
      return lifecycle;
    });
  }

  Future<PracticeWireResponse> _sendJson({
    required int generation,
    required String method,
    required String path,
    Map<String, Object?>? body,
    Map<String, String>? extraHeaders,
  }) {
    return _send(
      generation: generation,
      timeout: _jsonTimeout,
      method: method,
      path: path,
      jsonBody: body == null ? null : jsonEncode(body),
      extraHeaders: extraHeaders,
    );
  }

  Future<PracticeWireResponse> _send({
    required int generation,
    required Duration timeout,
    required String method,
    required String path,
    String? jsonBody,
    String? rawFilePath,
    Map<String, String>? extraHeaders,
  }) async {
    _requireGeneration(generation);
    final credential = _credentialProvider();
    if (credential == null) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
      );
    }
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(
      uri,
      sessionToken: credential.sessionToken,
    );
    try {
      final response = await _transport
          .send(
            PracticeWireRequest(
              method: method,
              uri: uri,
              headers: <String, String>{
                HttpHeaders.acceptHeader: ContentType.json.mimeType,
                HttpHeaders.authorizationHeader: bearerAuthorizationValue(
                  credential.sessionToken,
                ),
                if (jsonBody != null)
                  HttpHeaders.contentTypeHeader: ContentType.json.mimeType,
                ...?extraHeaders,
              },
              jsonBody: jsonBody,
              rawFilePath: rawFilePath,
            ),
          )
          .timeout(timeout);
      _requireGeneration(generation);
      if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
        throw const AgentClientOperationCancelled();
      }
      if (response.statusCode == HttpStatus.unauthorized) {
        unawaited(
          _invalidateSession(
            expectedSessionToken: credential.sessionToken,
            expectedGeneration: credential.generation,
          ),
        );
      }
      return response;
    } on TimeoutException {
      _requireGeneration(generation);
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        errorCode: 'practice_request_timed_out',
        retryable: true,
      );
    } on AgentClientException {
      rethrow;
    } on IOException {
      _requireGeneration(generation);
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } catch (_) {
      _requireGeneration(generation);
      throw const AgentClientException(kind: AgentClientFailureKind.unexpected);
    }
  }

  void _requireStatus(PracticeWireResponse response, Set<int> expected) {
    if (expected.contains(response.statusCode)) {
      return;
    }
    String? errorCode;
    String? correlationId;
    late final bool serverRetryable;
    try {
      final root = _exactObject(
        jsonDecode(response.body),
        required: const {'error'},
      );
      final error = _exactObject(
        root['error'],
        required: const {'code', 'message', 'retryable', 'correlation_id'},
        optional: const {'details'},
      );
      errorCode = _string(error, 'code', maxLength: 64);
      _string(error, 'message', maxLength: 512);
      serverRetryable = _boolean(error, 'retryable');
      correlationId = _string(error, 'correlation_id', maxLength: 128);
      if (error['details'] case final details?) {
        if (details is! List<Object?> || details.length > 32) {
          throw const FormatException();
        }
        for (final value in details) {
          final detail = _exactObject(
            value,
            required: const {'field', 'reason'},
          );
          _string(detail, 'field', maxLength: 128);
          _string(detail, 'reason', maxLength: 256);
        }
      }
    } catch (_) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidResponse,
      );
    }
    final retryAfter = _retryAfter(response.headers);
    throw AgentClientException(
      kind: switch (response.statusCode) {
        HttpStatus.badRequest => AgentClientFailureKind.invalidRequest,
        HttpStatus.unauthorized =>
          AgentClientFailureKind.authenticationRequired,
        HttpStatus.notFound => AgentClientFailureKind.notFound,
        HttpStatus.conflict => AgentClientFailureKind.conflict,
        HttpStatus.tooManyRequests => AgentClientFailureKind.rateLimited,
        >= 500 => AgentClientFailureKind.server,
        _ => AgentClientFailureKind.unexpected,
      },
      statusCode: response.statusCode,
      errorCode: errorCode,
      correlationId: correlationId,
      retryable: serverRetryable,
      retryAfter: retryAfter,
    );
  }

  Future<T> _run<T>(Future<T> Function(int generation) operation) {
    final generation = _accountGeneration;
    final completion = Completer<void>();
    _inFlight.add(completion.future);
    return Future<T>.sync(() => operation(generation)).whenComplete(() {
      _inFlight.remove(completion.future);
      completion.complete();
    });
  }

  void _requireGeneration(int generation) {
    if (generation != _accountGeneration) {
      throw const AgentClientOperationCancelled();
    }
  }
}

final class IoPracticeWireTransport implements PracticeWireTransport {
  IoPracticeWireTransport() : _client = HttpClient();

  static const _maximumResponseBytes = 1024 * 1024;

  final HttpClient _client;

  @override
  Future<PracticeWireResponse> send(PracticeWireRequest request) async {
    final ioRequest = await _client.openUrl(request.method, request.uri);
    ioRequest.followRedirects = false;
    request.headers.forEach(ioRequest.headers.set);
    final rawFilePath = request.rawFilePath;
    if (rawFilePath != null) {
      await ioRequest.addStream(File(rawFilePath).openRead());
    } else if (request.jsonBody != null) {
      ioRequest.add(utf8.encode(request.jsonBody!));
    }
    final response = await ioRequest.close();
    final bytes = BytesBuilder(copy: false);
    await for (final value in response) {
      if (bytes.length + value.length > _maximumResponseBytes) {
        throw const HttpException('Practice response exceeds size limit.');
      }
      bytes.add(value);
    }
    final responseHeaders = <String, String>{};
    response.headers.forEach((name, values) {
      responseHeaders[name] = values.join(',');
    });
    return PracticeWireResponse(
      statusCode: response.statusCode,
      body: utf8.decode(bytes.takeBytes()),
      headers: responseHeaders,
    );
  }

  @override
  void close({bool force = false}) => _client.close(force: force);
}

PracticeSessionSnapshot _decodeSessionState(
  String body, {
  String? expectedSessionId,
  String? expectedThreadId,
  String? expectedMatterId,
}) {
  final root = _exactObject(
    jsonDecode(body),
    required: const {
      'practice_session_id',
      'practice_plan_id',
      'thread_id',
      'matter',
      'session_version',
      'effective_turns',
      'turn_limit',
      'session_completed',
    },
    optional: const {'current_question', 'current_turn', 'review'},
  );
  final sessionId = _string(root, 'practice_session_id');
  final planId = _string(root, 'practice_plan_id');
  final threadId = _string(root, 'thread_id');
  final matter = _decodeMatter(_object(root['matter']));
  final sessionVersion = _integer(root, 'session_version');
  final effectiveTurns = _integer(root, 'effective_turns');
  final turnLimit = _integer(root, 'turn_limit');
  final completed = _boolean(root, 'session_completed');
  if (const {
    'current_question',
    'current_turn',
    'review',
  }.any((key) => root.containsKey(key) && root[key] == null)) {
    throw _invalidResponse();
  }
  final question = root['current_question'] == null
      ? null
      : _decodeQuestion(_object(root['current_question']));
  final turn = root['current_turn'] == null
      ? null
      : _decodeTurn(_object(root['current_turn']));
  final formalReview = root['review'] == null
      ? null
      : _decodeFormalReview(_object(root['review']));
  if ((expectedSessionId != null && sessionId != expectedSessionId) ||
      (expectedThreadId != null && threadId != expectedThreadId) ||
      (expectedMatterId != null && matter.id != expectedMatterId) ||
      sessionVersion < 1 ||
      effectiveTurns < 0 ||
      turnLimit < 1 ||
      turnLimit > 6 ||
      effectiveTurns > turnLimit ||
      (!completed && (question == null || formalReview != null)) ||
      (completed && (question != null || turn == null)) ||
      (question != null && question.sessionId != sessionId) ||
      (turn != null &&
          (turn.sessionId != sessionId ||
              turn.effectiveTurns != effectiveTurns ||
              turn.sessionCompleted != completed)) ||
      (formalReview != null &&
          (formalReview.sessionId != sessionId ||
              (turn != null && formalReview.sourceTurnId != turn.id) ||
              (turn != null &&
                  formalReview.sourceTurnVersion !=
                      'conversation-turn:evidence-v${turn.evidenceVersion}') ||
              (turn != null && formalReview.id != turn.reviewId)))) {
    throw _invalidResponse();
  }
  return PracticeSessionSnapshot(
    sessionId: sessionId,
    planId: planId,
    threadId: threadId,
    sessionVersion: sessionVersion,
    matter: matter,
    completedTurns: effectiveTurns,
    turnLimit: turnLimit,
    sessionCompleted: completed,
    currentQuestion: question,
    currentTurn: turn,
    review: formalReview?.presentation,
  );
}

PracticeSessionLifecycle _decodeSessionLifecycle(
  String body, {
  required String expectedSessionId,
}) {
  final root = _exactObject(
    jsonDecode(body),
    required: const {
      'practice_session_id',
      'practice_plan_id',
      'scenario_type',
      'scenario_model',
      'snapshot_id',
      'practice_session_status',
      'session_version',
      'created_at',
    },
    optional: const {'started_at', 'ended_at', 'end_reason'},
  );
  final sessionId = _string(root, 'practice_session_id');
  _string(root, 'practice_plan_id');
  _string(root, 'scenario_type', maxLength: 32);
  _string(root, 'scenario_model', maxLength: 64);
  _string(root, 'snapshot_id');
  final rawStatus = _string(root, 'practice_session_status', maxLength: 32);
  final status = switch (rawStatus) {
    'starting' => PracticeSessionLifecycleStatus.starting,
    'in_progress' => PracticeSessionLifecycleStatus.inProgress,
    'paused' => PracticeSessionLifecycleStatus.paused,
    'completed' => PracticeSessionLifecycleStatus.completed,
    'ended_early' => PracticeSessionLifecycleStatus.endedEarly,
    _ => throw _invalidResponse(),
  };
  final version = _integer(root, 'session_version');
  final createdAt = _dateTime(root, 'created_at');
  final startedAt = root.containsKey('started_at')
      ? _dateTime(root, 'started_at')
      : null;
  final endedAt = root.containsKey('ended_at')
      ? _dateTime(root, 'ended_at')
      : null;
  final endReason = root.containsKey('end_reason')
      ? _string(root, 'end_reason', maxLength: 64)
      : null;
  final terminal =
      status == PracticeSessionLifecycleStatus.completed ||
      status == PracticeSessionLifecycleStatus.endedEarly;
  if (sessionId != expectedSessionId ||
      version < 1 ||
      (startedAt != null && startedAt.isBefore(createdAt)) ||
      (terminal &&
          (startedAt == null || endedAt == null || endReason == null)) ||
      (!terminal && (endedAt != null || endReason != null)) ||
      (endedAt != null && endedAt.isBefore(startedAt!))) {
    throw _invalidResponse();
  }
  return PracticeSessionLifecycle(
    sessionId: sessionId,
    status: status,
    version: version,
  );
}

TranscriptionCandidate _decodeCandidate(
  String body, {
  required String expectedSessionId,
  required String expectedQuestionId,
}) {
  final root = _exactObject(
    jsonDecode(body),
    required: const {
      'candidate_id',
      'practice_session_id',
      'question_id',
      'respondent_participant_id',
      'transcript_id',
      'evidence_version',
      'transcript',
      'created_at',
    },
  );
  final candidate = TranscriptionCandidate(
    id: _string(root, 'candidate_id'),
    sessionId: _string(root, 'practice_session_id'),
    questionId: _string(root, 'question_id'),
    respondentParticipantId: _string(root, 'respondent_participant_id'),
    transcriptId: _string(root, 'transcript_id'),
    evidenceVersion: _integer(root, 'evidence_version'),
    text: _string(root, 'transcript'),
    createdAt: _dateTime(root, 'created_at'),
  );
  if (candidate.sessionId != expectedSessionId ||
      candidate.questionId != expectedQuestionId ||
      candidate.evidenceVersion! < 1) {
    throw _invalidResponse();
  }
  return candidate;
}

PracticeTurnConfirmation _confirmationFromState(
  PracticeSessionSnapshot state, {
  required String expectedQuestionId,
  String? expectedCandidateId,
}) {
  final turn = state.currentTurn;
  if (turn == null ||
      turn.questionId != expectedQuestionId ||
      (expectedCandidateId != null &&
          turn.candidateId != expectedCandidateId)) {
    throw _invalidResponse();
  }
  return PracticeTurnConfirmation(
    turnId: turn.id,
    sessionId: state.sessionId,
    questionId: turn.questionId,
    candidateId: turn.candidateId,
    answer: AgentMessage(
      id: turn.id,
      role: AgentMessageRole.user,
      text: turn.answerText,
    ),
    completedTurns: state.completedTurns,
    turnLimit: state.turnLimit,
    sessionCompleted: state.sessionCompleted,
    sessionVersion: state.sessionVersion,
    nextQuestion: state.currentQuestion,
    review: state.review,
    audioAssetId: turn.audioAssetId,
  );
}

AgentMatter _decodeMatter(Map<String, Object?> value) {
  final root = _exactObject(
    value,
    required: const {
      'matter_id',
      'title',
      'status',
      'version',
      'created_at',
      'updated_at',
    },
  );
  final id = _string(root, 'matter_id');
  final title = _string(root, 'title', maxLength: 256);
  final status = _string(root, 'status', maxLength: 32);
  final version = _integer(root, 'version');
  final createdAt = _dateTime(root, 'created_at');
  final updatedAt = _dateTime(root, 'updated_at');
  if (version < 1 || updatedAt.isBefore(createdAt)) {
    throw _invalidResponse();
  }
  final preset = agentScenes.where((scene) => scene.title == title).firstOrNull;
  return AgentMatter(
    id: id,
    scene:
        preset ??
        AgentScene(id: 'matter-$id', title: title, description: '自定义练习场景'),
    status: status,
    version: version,
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

PracticeQuestion _decodeQuestion(Map<String, Object?> value) {
  final root = _exactObject(
    value,
    required: const {
      'question_id',
      'practice_session_id',
      'content',
      'speaker_participant_id',
      'addressee_participant_ids',
      'speech_path',
    },
  );
  final addressees = _stringList(root, 'addressee_participant_ids');
  if (addressees.isEmpty || addressees.toSet().length != addressees.length) {
    throw _invalidResponse();
  }
  return PracticeQuestion(
    id: _string(root, 'question_id'),
    sessionId: _string(root, 'practice_session_id'),
    text: _string(root, 'content'),
    speakerParticipantId: _string(root, 'speaker_participant_id'),
    addresseeParticipantIds: addressees,
    speechPath: _string(root, 'speech_path'),
  );
}

PracticeTurnSnapshot _decodeTurn(Map<String, Object?> value) {
  final root = _exactObject(
    value,
    required: const {
      'turn_id',
      'practice_session_id',
      'question_id',
      'respondent_participant_id',
      'candidate_id',
      'answer_text',
      'evidence_version',
      'effective_turns',
      'session_completed',
    },
    optional: const {'review_id', 'audio_asset_id'},
  );
  if (root.containsKey('audio_asset_id') && root['audio_asset_id'] == null) {
    throw _invalidResponse();
  }
  final audioAssetId = root.containsKey('audio_asset_id')
      ? _string(root, 'audio_asset_id', maxLength: 128)
      : null;
  final turn = PracticeTurnSnapshot(
    id: _string(root, 'turn_id'),
    sessionId: _string(root, 'practice_session_id'),
    questionId: _string(root, 'question_id'),
    respondentParticipantId: _string(root, 'respondent_participant_id'),
    candidateId: _string(root, 'candidate_id'),
    answerText: _string(root, 'answer_text'),
    evidenceVersion: _integer(root, 'evidence_version'),
    effectiveTurns: _integer(root, 'effective_turns'),
    sessionCompleted: _boolean(root, 'session_completed'),
    reviewId: root.containsKey('review_id') ? _string(root, 'review_id') : null,
    audioAssetId: audioAssetId,
  );
  if (turn.evidenceVersion < 1 || turn.effectiveTurns < 1) {
    throw _invalidResponse();
  }
  return turn;
}

final class _FormalReviewProjection {
  const _FormalReviewProjection({
    required this.id,
    required this.sessionId,
    required this.status,
    required this.sourceTurnId,
    required this.sourceTurnVersion,
    this.presentation,
  });

  final String id;
  final String sessionId;
  final String status;
  final String sourceTurnId;
  final String sourceTurnVersion;
  final AgentReview? presentation;
}

_FormalReviewProjection _decodeFormalReview(Map<String, Object?> value) {
  final root = _exactObject(
    value,
    required: const {
      'review_id',
      'practice_session_id',
      'status',
      'implementation_version',
      'source_turn_id',
      'source_turn_version',
      'created_at',
      'updated_at',
    },
    optional: const {'result', 'completed_at'},
  );
  final id = _string(root, 'review_id');
  final sessionId = _string(root, 'practice_session_id');
  final status = _string(root, 'status', maxLength: 16);
  final sourceTurnVersion = _string(
    root,
    'source_turn_version',
    maxLength: 128,
  );
  if (!const {
        'pending',
        'generating',
        'completed',
        'failed',
      }.contains(status) ||
      !RegExp(
        r'^conversation-turn:evidence-v[1-9][0-9]*$',
      ).hasMatch(sourceTurnVersion)) {
    throw _invalidResponse();
  }
  _string(root, 'implementation_version', maxLength: 128);
  final sourceTurnId = _string(root, 'source_turn_id');
  final createdAt = _dateTime(root, 'created_at');
  final updatedAt = _dateTime(root, 'updated_at');
  if (updatedAt.isBefore(createdAt)) {
    throw _invalidResponse();
  }
  final result = root['result'];
  final completedAt = root['completed_at'];
  if ((root.containsKey('result') && result == null) ||
      (root.containsKey('completed_at') && completedAt == null)) {
    throw _invalidResponse();
  }
  AgentReview? presentation;
  if (status == 'completed') {
    if (result == null || completedAt == null) {
      throw _invalidResponse();
    }
    final completion = _dateTime(root, 'completed_at');
    if (completion.isBefore(createdAt)) {
      throw _invalidResponse();
    }
    presentation = _decodeReviewResult(id, _object(result));
  } else if (result != null || completedAt != null) {
    throw _invalidResponse();
  }
  return _FormalReviewProjection(
    id: id,
    sessionId: sessionId,
    status: status,
    sourceTurnId: sourceTurnId,
    sourceTurnVersion: sourceTurnVersion,
    presentation: presentation,
  );
}

AgentReview _decodeReviewResult(String reviewId, Map<String, Object?> value) {
  final root = _exactObject(
    value,
    required: const {'overall_score', 'summary', 'conclusions'},
  );
  final score = _integer(root, 'overall_score');
  final summary = _string(root, 'summary');
  final conclusions = _objectList(
    root,
    'conclusions',
  ).map(_decodeConclusion).toList(growable: false);
  if (score < 0 || score > 100) {
    throw _invalidResponse();
  }
  final strength = conclusions.firstOrNull?.message ?? summary;
  final nextFocus =
      conclusions
          .where((item) => item.suggestion != null)
          .map((item) => item.suggestion!)
          .firstOrNull ??
      conclusions.lastOrNull?.message ??
      summary;
  return AgentReview(
    id: reviewId,
    title: '本次练习 · $score 分',
    summary: summary,
    strength: strength,
    nextFocus: nextFocus,
  );
}

final class _ReviewConclusion {
  const _ReviewConclusion({required this.message, this.suggestion});

  final String message;
  final String? suggestion;
}

_ReviewConclusion _decodeConclusion(Map<String, Object?> value) {
  final root = _exactObject(
    value,
    required: const {'key', 'category', 'message'},
    optional: const {'suggestion'},
  );
  _string(root, 'key', maxLength: 128);
  _string(root, 'category', maxLength: 128);
  return _ReviewConclusion(
    message: _string(root, 'message'),
    suggestion: root.containsKey('suggestion')
        ? _string(root, 'suggestion')
        : null,
  );
}

Map<String, Object?> _exactObject(
  Object? value, {
  Set<String> required = const {},
  Set<String> optional = const {},
}) {
  final object = _object(value);
  final allowed = {...required, ...optional};
  if (!object.keys.toSet().containsAll(required) ||
      object.keys.any((key) => !allowed.contains(key))) {
    throw _invalidResponse();
  }
  return object;
}

Map<String, Object?> _object(Object? value) {
  if (value is! Map<String, Object?>) {
    throw _invalidResponse();
  }
  return value;
}

String _string(Map<String, Object?> value, String key, {int maxLength = 4096}) {
  final item = value[key];
  if (item is! String ||
      item.trim().isEmpty ||
      item.length > maxLength ||
      item.contains('\u0000')) {
    throw _invalidResponse();
  }
  return item;
}

int _integer(Map<String, Object?> value, String key) {
  final item = value[key];
  if (item is! int) {
    throw _invalidResponse();
  }
  return item;
}

bool _boolean(Map<String, Object?> value, String key) {
  final item = value[key];
  if (item is! bool) {
    throw _invalidResponse();
  }
  return item;
}

DateTime _dateTime(Map<String, Object?> value, String key) {
  final parsed = DateTime.tryParse(_string(value, key, maxLength: 64));
  if (parsed == null || !parsed.isUtc) {
    throw _invalidResponse();
  }
  return parsed;
}

List<String> _stringList(Map<String, Object?> value, String key) {
  final list = value[key];
  if (list is! List<Object?> || list.length > 100) {
    throw _invalidResponse();
  }
  return [
    for (final item in list)
      _string(<String, Object?>{'value': item}, 'value', maxLength: 256),
  ];
}

List<Map<String, Object?>> _objectList(Map<String, Object?> value, String key) {
  final list = value[key];
  if (list is! List<Object?> || list.length > 100) {
    throw _invalidResponse();
  }
  return [for (final item in list) _object(item)];
}

AgentClientException _invalidResponse() {
  return const AgentClientException(
    kind: AgentClientFailureKind.invalidResponse,
    retryable: true,
  );
}

Duration? _retryAfter(Map<String, String> headers) {
  final value = headers.entries
      .where((entry) => entry.key.toLowerCase() == 'retry-after')
      .map((entry) => entry.value.trim())
      .firstOrNull;
  if (value == null || value.isEmpty) {
    return null;
  }
  final seconds = int.tryParse(value);
  if (seconds != null && seconds >= 0) {
    return Duration(seconds: seconds);
  }
  try {
    final date = HttpDate.parse(value);
    final delay = date.difference(DateTime.now().toUtc());
    return delay.isNegative ? Duration.zero : delay;
  } on FormatException {
    return null;
  }
}

void _requireOpaqueId(String value) {
  if (value.trim().isEmpty ||
      value.length > 256 ||
      value.contains('\u0000') ||
      value.contains('\r') ||
      value.contains('\n')) {
    throw ArgumentError.value(value, 'value', 'Invalid opaque resource ID.');
  }
}

void _requireClientId(String value) {
  if (value.length < 8 || value.length > 128) {
    throw ArgumentError.value(value, 'value', 'Invalid Idempotency-Key.');
  }
  _requireOpaqueId(value);
}
