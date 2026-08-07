import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/features/coaching/practice/practice_client_error.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment_codec.dart';

import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/bearer_authentication.dart';
import 'package:speakup/identity/network/transport_security.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';
import 'package:speakup/features/coaching/practice/practice_recording.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';

/// The single replaceable location for voice-practice routes.
///
/// UI and Controller code depend only on [PracticeClient].
final class PracticeWireEndpoints {
  const PracticeWireEndpoints({
    this.voiceActivation =
        '/v1/practice-sessions/{practice_session_id}/voice-activation',
    this.voiceState = '/v1/practice-sessions/{practice_session_id}/voice-state',
    this.transcribe =
        '/v1/voice-practice-sessions/{practice_session_id}/questions/'
        '{question_id}/transcription-candidates',
    this.submitText =
        '/v1/voice-practice-sessions/{practice_session_id}/questions/'
        '{question_id}/text-answers',
    this.questionTip =
        '/v1/voice-practice-sessions/{practice_session_id}/questions/'
        '{question_id}/tips',
    this.confirm = '/v1/transcription-candidates/{candidate_id}/confirmations',
    this.endEarly = '/v1/practice-sessions/{practice_session_id}/end-early',
    this.complete = '/v1/practice-sessions/{practice_session_id}/complete',
    this.retryRequest = '/v1/feedback-items/{feedback_item_id}/retry-requests',
    this.retryRequestStatus = '/v1/retry-requests/{retry_request_id}',
    this.retryConfirmation =
        '/v1/retry-turns/{retry_turn_id}/transcription-candidates/'
        '{candidate_id}/confirmations',
    this.questionTranslation = '/v1/voice-questions/{question_id}/translation',
  });

  final String voiceActivation;
  final String voiceState;
  final String transcribe;
  final String submitText;
  final String questionTip;
  final String confirm;
  final String endEarly;
  final String complete;
  final String retryRequest;
  final String retryRequestStatus;
  final String retryConfirmation;
  final String questionTranslation;

  String voiceActivationPath(String sessionId) => voiceActivation.replaceAll(
    '{practice_session_id}',
    _pathSegment(sessionId),
  );

  String voiceStatePath(String sessionId) =>
      voiceState.replaceAll('{practice_session_id}', _pathSegment(sessionId));

  String transcribePath(String sessionId, String questionId) => transcribe
      .replaceAll('{practice_session_id}', _pathSegment(sessionId))
      .replaceAll('{question_id}', _pathSegment(questionId));

  String confirmPath(String candidateId) =>
      confirm.replaceAll('{candidate_id}', _pathSegment(candidateId));

  String submitTextPath(String sessionId, String questionId) => submitText
      .replaceAll('{practice_session_id}', _pathSegment(sessionId))
      .replaceAll('{question_id}', _pathSegment(questionId));

  String questionTranslationPath(String questionId) =>
      questionTranslation.replaceAll('{question_id}', _pathSegment(questionId));

  String questionTipPath(String sessionId, String questionId) => questionTip
      .replaceAll('{practice_session_id}', _pathSegment(sessionId))
      .replaceAll('{question_id}', _pathSegment(questionId));

  String endEarlyPath(String sessionId) =>
      endEarly.replaceAll('{practice_session_id}', _pathSegment(sessionId));

  String completePath(String sessionId) =>
      complete.replaceAll('{practice_session_id}', _pathSegment(sessionId));

  String retryRequestPath(String feedbackItemId) => retryRequest.replaceAll(
    '{feedback_item_id}',
    _pathSegment(feedbackItemId),
  );

  String retryRequestStatusPath(String retryRequestId) => retryRequestStatus
      .replaceAll('{retry_request_id}', _pathSegment(retryRequestId));

  String retryConfirmationPath(String retryTurnId, String candidateId) =>
      retryConfirmation
          .replaceAll('{retry_turn_id}', _pathSegment(retryTurnId))
          .replaceAll('{candidate_id}', _pathSegment(candidateId));
}

String _pathSegment(String value) => Uri.encodeComponent(value);

final _retryRequestStatusPathPattern = RegExp(
  r'^/v1/retry-requests/([A-Za-z0-9._~-]{1,128})$',
);
final _retryAnswerPathPattern = RegExp(
  r'^/v1/retry-turns/([A-Za-z0-9._~-]{1,128})/transcription-candidates$',
);

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
    implements
        PracticeClient,
        PracticeLifecycleClient,
        PracticeCompletionClient,
        PracticeSpeechFeedbackRetryClient,
        PracticeQuestionTipClient,
        PracticeQuestionTranslationClient {
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

  // Matches the server-owned media contract. A 120-second, 16 kHz, mono WAV
  // is about 3.84 MB and must remain uploadable for IELTS Part 2.
  static const _maximumAudioBytes = 7_400_000;

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
  Future<PracticeSessionSnapshot> restorePractice({required String sessionId}) {
    return _run((generation) async {
      _requireOpaqueId(sessionId);
      final response = await _sendJson(
        generation: generation,
        method: 'GET',
        path: _endpoints.voiceStatePath(sessionId),
      );
      _requireStatus(response, const {HttpStatus.ok});
      return _decodeSessionState(response.body, expectedSessionId: sessionId);
    });
  }

  @override
  Future<PracticeSessionSnapshot> activatePractice({
    required String sessionId,
    required String clientOperationId,
  }) {
    return _run((generation) async {
      _requireOpaqueId(sessionId);
      _requireClientId(clientOperationId);
      final response = await _send(
        generation: generation,
        timeout: _jsonTimeout,
        method: 'POST',
        path: _endpoints.voiceActivationPath(sessionId),
        extraHeaders: <String, String>{'Idempotency-Key': clientOperationId},
      );
      _requireStatus(response, const {HttpStatus.ok});
      return _decodeSessionState(response.body, expectedSessionId: sessionId);
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
      await _validateAudio(audio);
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
  Future<PracticeRetryRequest> requestSameQuestionRetry({
    required String feedbackItemId,
    required String idempotencyKey,
  }) {
    return _run((generation) async {
      _requireOpaqueId(feedbackItemId);
      _requireClientId(idempotencyKey);
      final response = await _send(
        generation: generation,
        timeout: _jsonTimeout,
        method: 'POST',
        path: _endpoints.retryRequestPath(feedbackItemId),
        extraHeaders: <String, String>{'Idempotency-Key': idempotencyKey},
      );
      _requireStatus(response, const {HttpStatus.created, HttpStatus.ok});
      return _decodeRetryRequest(
        response.body,
        expectedFeedbackItemId: feedbackItemId,
      );
    });
  }

  @override
  Future<PracticeRetryRequest> getSameQuestionRetryRequest({
    required String retryRequestId,
  }) {
    return _run((generation) async {
      _requireOpaqueId(retryRequestId);
      final response = await _sendJson(
        generation: generation,
        method: 'GET',
        path: _endpoints.retryRequestStatusPath(retryRequestId),
      );
      _requireStatus(response, const {HttpStatus.ok});
      return _decodeRetryRequest(
        response.body,
        expectedRetryRequestId: retryRequestId,
      );
    });
  }

  @override
  Future<RetryTranscriptionCandidate> transcribeRetry({
    required String answerPath,
    required String idempotencyKey,
    required RecordedPracticeAudio audio,
  }) {
    return _run((generation) async {
      final retryTurnId = _requireRetryAnswerPath(answerPath);
      _requireClientId(idempotencyKey);
      await _validateAudio(audio);
      final response = await _send(
        generation: generation,
        timeout: _transcriptionTimeout,
        method: 'POST',
        path: answerPath,
        extraHeaders: <String, String>{
          'Idempotency-Key': idempotencyKey,
          HttpHeaders.contentTypeHeader: 'audio/wav',
        },
        rawFilePath: audio.path,
      );
      _requireStatus(response, const {HttpStatus.created});
      return _decodeRetryCandidate(
        response.body,
        expectedRetryTurnId: retryTurnId,
      );
    });
  }

  @override
  Future<ConfirmedRetryTurn> confirmRetry({
    required String retryTurnId,
    required String candidateId,
    required String idempotencyKey,
  }) {
    return _run((generation) async {
      _requireOpaqueId(retryTurnId);
      _requireOpaqueId(candidateId);
      _requireClientId(idempotencyKey);
      final response = await _send(
        generation: generation,
        timeout: _jsonTimeout,
        method: 'POST',
        path: _endpoints.retryConfirmationPath(retryTurnId, candidateId),
        extraHeaders: <String, String>{'Idempotency-Key': idempotencyKey},
      );
      _requireStatus(response, const {HttpStatus.ok});
      return _decodeConfirmedRetryTurn(
        response.body,
        expectedRetryTurnId: retryTurnId,
        expectedCandidateId: candidateId,
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
        throw const PracticeClientException(
          kind: PracticeClientFailureKind.invalidRequest,
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
  Future<PracticeQuestionTranslation> translateQuestion({
    required String questionId,
  }) {
    return _run((generation) async {
      _requireOpaqueId(questionId);
      final response = await _send(
        generation: generation,
        timeout: _jsonTimeout,
        method: 'GET',
        path: _endpoints.questionTranslationPath(questionId),
      );
      _requireStatus(response, const {HttpStatus.ok});
      return _decodeQuestionTranslation(
        response.body,
        expectedQuestionId: questionId,
      );
    });
  }

  @override
  Future<PracticeQuestionTip> ensureQuestionTip({
    required String sessionId,
    required String questionId,
    required String idempotencyKey,
  }) {
    return _run((generation) async {
      _requireOpaqueId(sessionId);
      _requireOpaqueId(questionId);
      _requireClientId(idempotencyKey);
      final response = await _send(
        generation: generation,
        timeout: _jsonTimeout,
        method: 'POST',
        path: _endpoints.questionTipPath(sessionId, questionId),
        extraHeaders: <String, String>{'Idempotency-Key': idempotencyKey},
      );
      _requireStatus(response, const {HttpStatus.ok});
      return _decodeQuestionTip(
        response.body,
        expectedSessionId: sessionId,
        expectedQuestionId: questionId,
      );
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
        throw const PracticeClientException(
          kind: PracticeClientFailureKind.invalidRequest,
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

  @override
  Future<PracticeSessionLifecycle> complete({
    required String sessionId,
    required int expectedSessionVersion,
    required String idempotencyKey,
  }) {
    return _run((generation) async {
      _requireOpaqueId(sessionId);
      _requireClientId(idempotencyKey);
      if (expectedSessionVersion < 1) {
        throw const PracticeClientException(
          kind: PracticeClientFailureKind.invalidRequest,
        );
      }
      final response = await _sendJson(
        generation: generation,
        method: 'POST',
        path: _endpoints.completePath(sessionId),
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
      if (lifecycle.status != PracticeSessionLifecycleStatus.completed ||
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

  Future<void> _validateAudio(RecordedPracticeAudio audio) async {
    if (audio.contentType != 'audio/wav' ||
        audio.sizeBytes < 45 ||
        audio.sizeBytes > _maximumAudioBytes) {
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.invalidRequest,
        errorCode: 'invalid_audio',
      );
    }
    final audioType = await FileSystemEntity.type(
      audio.path,
      followLinks: false,
    );
    if (audioType != FileSystemEntityType.file ||
        await File(audio.path).length() != audio.sizeBytes) {
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.invalidRequest,
        errorCode: 'invalid_audio',
      );
    }
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
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.authenticationRequired,
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
        throw const PracticeClientOperationCancelled();
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
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.network,
        errorCode: 'practice_request_timed_out',
        retryable: true,
      );
    } on PracticeClientException {
      rethrow;
    } on IOException {
      _requireGeneration(generation);
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.network,
        retryable: true,
      );
    } catch (_) {
      _requireGeneration(generation);
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.unexpected,
      );
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
      throw const PracticeClientException(
        kind: PracticeClientFailureKind.invalidResponse,
      );
    }
    final retryAfter = _retryAfter(response.headers);
    throw PracticeClientException(
      kind: switch (response.statusCode) {
        HttpStatus.badRequest => PracticeClientFailureKind.invalidRequest,
        HttpStatus.unauthorized =>
          PracticeClientFailureKind.authenticationRequired,
        HttpStatus.notFound => PracticeClientFailureKind.notFound,
        HttpStatus.conflict => PracticeClientFailureKind.conflict,
        HttpStatus.tooManyRequests => PracticeClientFailureKind.rateLimited,
        >= 500 => PracticeClientFailureKind.server,
        _ => PracticeClientFailureKind.unexpected,
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
      throw const PracticeClientOperationCancelled();
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
}) {
  final root = _exactObject(
    jsonDecode(body),
    required: const {
      'practice_session_id',
      'practice_plan_id',
      'scene_id',
      'scene_version',
      'practice_experience',
      'scene_category',
      'practice_mode',
      'practice_session_status',
      'practice_capabilities',
      'session_version',
      'effective_turns',
      'turn_limit',
      'session_completed',
    },
    optional: const {
      'ielts_assignment',
      'current_question',
      'current_turn',
      'turn_history',
      'completion_mode',
    },
  );
  final sessionId = _string(root, 'practice_session_id');
  final planId = _string(root, 'practice_plan_id');
  _string(root, 'scene_id');
  final sceneVersion = _integer(root, 'scene_version');
  final practiceExperience = PracticeExperience.fromWireValue(
    _string(root, 'practice_experience', maxLength: 32),
  );
  final sceneCategory = SceneCategory.fromWireValue(
    _string(root, 'scene_category', maxLength: 64),
  );
  final practiceMode = PracticeMode.fromWireValue(
    _string(root, 'practice_mode', maxLength: 32),
  );
  final capabilities = _decodePracticeCapabilities(
    _object(root['practice_capabilities']),
  );
  final sessionStatus = switch (_string(
    root,
    'practice_session_status',
    maxLength: 32,
  )) {
    'in_progress' => PracticeSessionLifecycleStatus.inProgress,
    'paused' => PracticeSessionLifecycleStatus.paused,
    'completed' => PracticeSessionLifecycleStatus.completed,
    'ended_early' => PracticeSessionLifecycleStatus.endedEarly,
    _ => throw _invalidResponse(),
  };
  final sessionVersion = _integer(root, 'session_version');
  final effectiveTurns = _integer(root, 'effective_turns');
  final turnLimit = _integer(root, 'turn_limit');
  final completionMode = root.containsKey('completion_mode')
      ? PracticeCompletionMode.fromWireValue(
          _string(root, 'completion_mode', maxLength: 32),
        )
      : PracticeCompletionMode.turnLimited;
  final completed = _boolean(root, 'session_completed');
  final terminal =
      sessionStatus == PracticeSessionLifecycleStatus.completed ||
      sessionStatus == PracticeSessionLifecycleStatus.endedEarly;
  final ieltsAssignment = root.containsKey('ielts_assignment')
      ? _decodeIeltsAssignment(root['ielts_assignment'])
      : null;
  if (const {
    'ielts_assignment',
    'current_question',
    'current_turn',
    'turn_history',
  }.any((key) => root.containsKey(key) && root[key] == null)) {
    throw _invalidResponse();
  }
  final question = root['current_question'] == null
      ? null
      : _decodeQuestion(_object(root['current_question']));
  final turn = root['current_turn'] == null
      ? null
      : _decodeTurn(_object(root['current_turn']));
  final turnHistory = root['turn_history'] == null
      ? const <PracticeTurnExchange>[]
      : _decodeTurnHistory(root['turn_history']);
  final historyEffectiveTurns = turnHistory
      .where((exchange) => exchange.turn.countsTowardEffectiveTurnLimit)
      .length;
  if ((expectedSessionId != null && sessionId != expectedSessionId) ||
      practiceExperience == null ||
      sceneCategory == null ||
      practiceMode == null ||
      sceneVersion < 1 ||
      sessionVersion < 1 ||
      completed != terminal ||
      effectiveTurns < 0 ||
      completionMode == null ||
      (completionMode == PracticeCompletionMode.turnLimited &&
          (turnLimit < 1 ||
              turnLimit > practiceTurnSafetyLimit ||
              effectiveTurns > turnLimit)) ||
      (completionMode == PracticeCompletionMode.userControlled &&
          turnLimit != 0) ||
      (practiceExperience == PracticeExperience.ieltsSpeaking) !=
          (ieltsAssignment != null) ||
      (ieltsAssignment != null &&
          (ieltsAssignment.mode != practiceMode ||
              ieltsAssignment.turnBlueprints.length != turnLimit)) ||
      (!completed && question == null) ||
      (completed &&
          (question != null || (effectiveTurns > 0 && turn == null))) ||
      (question != null && question.sessionId != sessionId) ||
      (turn != null &&
          (turn.sessionId != sessionId ||
              turn.effectiveTurns != effectiveTurns ||
              turn.sessionCompleted != completed)) ||
      (capabilities.speechFeedbackAllowed &&
          effectiveTurns > 0 &&
          !root.containsKey('turn_history')) ||
      (turnHistory.isNotEmpty &&
          (historyEffectiveTurns != effectiveTurns ||
              turnHistory.last.turn.id != turn?.id ||
              turnHistory.any(
                (exchange) =>
                    exchange.question.sessionId != sessionId ||
                    exchange.turn.sessionId != sessionId ||
                    exchange.question.id != exchange.turn.questionId,
              )))) {
    throw _invalidResponse();
  }
  return PracticeSessionSnapshot(
    sessionId: sessionId,
    planId: planId,
    practiceExperience: practiceExperience,
    sceneCategory: sceneCategory,
    practiceMode: practiceMode,
    capabilities: capabilities,
    sessionVersion: sessionVersion,
    completedTurns: effectiveTurns,
    turnLimit: turnLimit,
    completionMode: completionMode,
    sessionCompleted: completed,
    ieltsAssignment: ieltsAssignment,
    currentQuestion: question,
    currentTurn: turn,
    turnHistory: turnHistory,
  );
}

IeltsPracticeAssignment _decodeIeltsAssignment(Object? value) {
  try {
    return decodeIeltsAssignment(value);
  } on IeltsAssignmentWireFormatException {
    throw _invalidResponse();
  }
}

List<PracticeTurnExchange> _decodeTurnHistory(Object? value) {
  if (value is! List<Object?> || value.isEmpty) {
    throw _invalidResponse();
  }
  final exchanges = <PracticeTurnExchange>[];
  final questionIds = <String>{};
  final turnIds = <String>{};
  final primaryQuestionIds = <String>{};
  var effectiveTurns = 0;
  for (var index = 0; index < value.length; index++) {
    final root = _exactObject(
      value[index],
      required: const {'question', 'turn'},
    );
    final question = _decodeQuestion(_object(root['question']));
    final turn = _decodeTurn(_object(root['turn']));
    if (turn.countsTowardEffectiveTurnLimit) {
      effectiveTurns++;
    }
    if (!questionIds.add(question.id) ||
        !turnIds.add(turn.id) ||
        turn.effectiveTurns != effectiveTurns ||
        question.id != turn.questionId ||
        question.isFollowUp == turn.countsTowardEffectiveTurnLimit ||
        (question.isFollowUp &&
            !primaryQuestionIds.contains(question.parentQuestionId))) {
      throw _invalidResponse();
    }
    if (!question.isFollowUp) {
      primaryQuestionIds.add(question.id);
    }
    exchanges.add(PracticeTurnExchange(question: question, turn: turn));
  }
  return List<PracticeTurnExchange>.unmodifiable(exchanges);
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
      'plan_revision',
      'practice_experience',
      'scene_category',
      'practice_mode',
      'evaluation_policy_ref',
      'snapshot_id',
      'practice_session_status',
      'session_version',
      'created_at',
    },
    optional: const {'started_at', 'ended_at', 'end_reason'},
  );
  final sessionId = _string(root, 'practice_session_id');
  _string(root, 'practice_plan_id');
  final planRevision = _integer(root, 'plan_revision');
  _string(root, 'practice_experience', maxLength: 32);
  _string(root, 'scene_category', maxLength: 64);
  _string(root, 'practice_mode', maxLength: 32);
  _string(root, 'evaluation_policy_ref');
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
      planRevision < 1 ||
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

PracticeQuestionTip _decodeQuestionTip(
  String body, {
  required String expectedSessionId,
  required String expectedQuestionId,
}) {
  final root = _exactObject(
    jsonDecode(body),
    required: const {
      'tip_id',
      'practice_session_id',
      'question_id',
      'content',
      'created_at',
    },
  );
  final tip = PracticeQuestionTip(
    id: _string(root, 'tip_id'),
    sessionId: _string(root, 'practice_session_id'),
    questionId: _string(root, 'question_id'),
    content: _utf8String(root, 'content', maxLength: 800, maxBytes: 2400),
    createdAt: _dateTime(root, 'created_at'),
  );
  if (tip.sessionId != expectedSessionId ||
      tip.questionId != expectedQuestionId) {
    throw _invalidResponse();
  }
  return tip;
}

PracticeRetryRequest _decodeRetryRequest(
  String body, {
  String? expectedRetryRequestId,
  String? expectedFeedbackItemId,
}) {
  final root = _exactObject(
    jsonDecode(body),
    required: const {
      'retry_request_id',
      'feedback_item_id',
      'practice_session_id',
      'original_turn_id',
      'question_id',
      'retry_status',
      'status_url',
      'created_at',
      'updated_at',
    },
    optional: const {
      'new_turn_id',
      'new_turn_status',
      'answer_path',
      'stable_failure',
      'completed_at',
    },
  );
  final retryRequestId = _string(root, 'retry_request_id', maxLength: 128);
  final feedbackItemId = _string(root, 'feedback_item_id', maxLength: 128);
  final sessionId = _string(root, 'practice_session_id', maxLength: 128);
  final originalTurnId = _string(root, 'original_turn_id', maxLength: 128);
  final questionId = _string(root, 'question_id', maxLength: 128);
  final retryStatus = switch (_string(root, 'retry_status', maxLength: 32)) {
    'PENDING' => PracticeRetryRequestStatus.pending,
    'TURN_CREATED' => PracticeRetryRequestStatus.turnCreated,
    'FAILED' => PracticeRetryRequestStatus.failed,
    _ => throw _invalidResponse(),
  };
  final statusUrl = _string(root, 'status_url', maxLength: 256);
  final createdAt = _dateTime(root, 'created_at');
  final updatedAt = _dateTime(root, 'updated_at');
  final newTurnId = root.containsKey('new_turn_id')
      ? _string(root, 'new_turn_id', maxLength: 128)
      : null;
  final newTurnStatus = root.containsKey('new_turn_status')
      ? _string(root, 'new_turn_status', maxLength: 32)
      : null;
  final answerPath = root.containsKey('answer_path')
      ? _string(root, 'answer_path', maxLength: 512)
      : null;
  final stableFailure = root.containsKey('stable_failure')
      ? _decodePracticeRetryFailure(root['stable_failure'])
      : null;
  final completedAt = root.containsKey('completed_at')
      ? _dateTime(root, 'completed_at')
      : null;

  final statusUrlId = _retryRequestIdFromStatusUrl(statusUrl);
  final answerPathTurnId = answerPath == null
      ? null
      : _retryTurnIdFromAnswerPath(answerPath);
  final validShape = switch (retryStatus) {
    PracticeRetryRequestStatus.pending =>
      newTurnId == null &&
          newTurnStatus == null &&
          answerPath == null &&
          stableFailure == null &&
          completedAt == null,
    PracticeRetryRequestStatus.turnCreated =>
      newTurnId != null &&
          newTurnStatus == 'ANSWERING' &&
          answerPath != null &&
          answerPathTurnId == newTurnId &&
          stableFailure == null &&
          completedAt != null,
    PracticeRetryRequestStatus.failed =>
      newTurnId == null &&
          newTurnStatus == null &&
          answerPath == null &&
          stableFailure != null &&
          completedAt != null,
  };
  if ((expectedRetryRequestId != null &&
          retryRequestId != expectedRetryRequestId) ||
      (expectedFeedbackItemId != null &&
          feedbackItemId != expectedFeedbackItemId) ||
      statusUrlId != retryRequestId ||
      updatedAt.isBefore(createdAt) ||
      (completedAt != null && completedAt.isBefore(createdAt)) ||
      !validShape) {
    throw _invalidResponse();
  }
  return PracticeRetryRequest(
    retryRequestId: retryRequestId,
    feedbackItemId: feedbackItemId,
    sessionId: sessionId,
    originalTurnId: originalTurnId,
    questionId: questionId,
    retryStatus: retryStatus,
    statusUrl: statusUrl,
    createdAt: createdAt,
    updatedAt: updatedAt,
    newTurnId: newTurnId,
    answerPath: answerPath,
    stableFailure: stableFailure,
    completedAt: completedAt,
  );
}

PracticeQuestionTranslation _decodeQuestionTranslation(
  String body, {
  required String expectedQuestionId,
}) {
  final root = _exactObject(
    jsonDecode(body),
    required: const {'question_id', 'target_language', 'translation'},
  );
  final translation = PracticeQuestionTranslation(
    questionId: _string(root, 'question_id', maxLength: 128),
    targetLanguage: _string(root, 'target_language', maxLength: 16),
    content: _string(root, 'translation', maxLength: 2000),
  );
  if (translation.questionId != expectedQuestionId ||
      translation.targetLanguage != 'zh-CN' ||
      translation.content.trim().isEmpty) {
    throw _invalidResponse();
  }
  return translation;
}

PracticeRetryFailure _decodePracticeRetryFailure(Object? value) {
  final root = _exactObject(
    value,
    required: const {'reason_code', 'retryable'},
  );
  final reason = switch (_string(root, 'reason_code', maxLength: 64)) {
    'SOURCE_NO_LONGER_AVAILABLE' =>
      PracticeRetryFailureReason.sourceNoLongerAvailable,
    'RETRY_TURN_CREATION_FAILED' =>
      PracticeRetryFailureReason.retryTurnCreationFailed,
    _ => throw _invalidResponse(),
  };
  final retryable = _boolean(root, 'retryable');
  if ((reason == PracticeRetryFailureReason.sourceNoLongerAvailable &&
          retryable) ||
      (reason == PracticeRetryFailureReason.retryTurnCreationFailed &&
          !retryable)) {
    throw _invalidResponse();
  }
  return PracticeRetryFailure(reason: reason, retryable: retryable);
}

RetryTranscriptionCandidate _decodeRetryCandidate(
  String body, {
  required String expectedRetryTurnId,
}) {
  final root = _exactObject(
    jsonDecode(body),
    required: const {
      'candidate_id',
      'retry_turn_id',
      'retry_request_id',
      'practice_session_id',
      'question_id',
      'respondent_participant_id',
      'candidate_status',
      'transcript_id',
      'evidence_version',
      'transcript',
      'created_at',
    },
  );
  final retryTurnId = _string(root, 'retry_turn_id', maxLength: 128);
  final candidateStatus = _string(root, 'candidate_status', maxLength: 32);
  final evidenceVersion = _integer(root, 'evidence_version');
  if (retryTurnId != expectedRetryTurnId ||
      candidateStatus != 'READY' ||
      evidenceVersion < 1) {
    throw _invalidResponse();
  }
  return RetryTranscriptionCandidate(
    id: _string(root, 'candidate_id', maxLength: 128),
    retryTurnId: retryTurnId,
    retryRequestId: _string(root, 'retry_request_id', maxLength: 128),
    sessionId: _string(root, 'practice_session_id', maxLength: 128),
    questionId: _string(root, 'question_id', maxLength: 128),
    respondentParticipantId: _string(
      root,
      'respondent_participant_id',
      maxLength: 128,
    ),
    transcriptId: _string(root, 'transcript_id', maxLength: 128),
    evidenceVersion: evidenceVersion,
    text: _utf8String(root, 'transcript', maxLength: 4096, maxBytes: 16384),
    createdAt: _dateTime(root, 'created_at'),
  );
}

ConfirmedRetryTurn _decodeConfirmedRetryTurn(
  String body, {
  required String expectedRetryTurnId,
  required String expectedCandidateId,
}) {
  final root = _exactObject(
    jsonDecode(body),
    required: const {
      'turn_id',
      'retry_request_id',
      'original_turn_id',
      'practice_session_id',
      'question_id',
      'respondent_participant_id',
      'candidate_id',
      'interaction_mode',
      'answer_text',
      'evidence_version',
      'turn_kind',
      'turn_status',
      'counts_toward_turn_limit',
      'created_at',
      'confirmed_at',
    },
    optional: const {'audio_asset_id'},
  );
  final turnId = _string(root, 'turn_id', maxLength: 128);
  final candidateId = _string(root, 'candidate_id', maxLength: 128);
  final evidenceVersion = _integer(root, 'evidence_version');
  final createdAt = _dateTime(root, 'created_at');
  final confirmedAt = _dateTime(root, 'confirmed_at');
  final countsTowardTurnLimit = _boolean(root, 'counts_toward_turn_limit');
  if (turnId != expectedRetryTurnId ||
      candidateId != expectedCandidateId ||
      _string(root, 'interaction_mode', maxLength: 32) != 'PUSH_TO_TALK' ||
      _string(root, 'turn_kind', maxLength: 32) != 'RETRY' ||
      _string(root, 'turn_status', maxLength: 32) != 'CONFIRMED' ||
      countsTowardTurnLimit ||
      evidenceVersion < 1 ||
      confirmedAt.isBefore(createdAt)) {
    throw _invalidResponse();
  }
  return ConfirmedRetryTurn(
    turnId: turnId,
    retryRequestId: _string(root, 'retry_request_id', maxLength: 128),
    originalTurnId: _string(root, 'original_turn_id', maxLength: 128),
    sessionId: _string(root, 'practice_session_id', maxLength: 128),
    questionId: _string(root, 'question_id', maxLength: 128),
    respondentParticipantId: _string(
      root,
      'respondent_participant_id',
      maxLength: 128,
    ),
    candidateId: candidateId,
    answerText: _utf8String(
      root,
      'answer_text',
      maxLength: 4096,
      maxBytes: 16384,
    ),
    evidenceVersion: evidenceVersion,
    countsTowardTurnLimit: countsTowardTurnLimit,
    audioAssetId: root.containsKey('audio_asset_id')
        ? _string(root, 'audio_asset_id', maxLength: 128)
        : null,
    createdAt: createdAt,
    confirmedAt: confirmedAt,
  );
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
    answer: PracticeMessage(
      id: turn.id,
      role: PracticeMessageRole.user,
      text: turn.answerText,
      speechFeedbackStatusUrl: turn.speechFeedbackStatusUrl,
    ),
    completedTurns: state.completedTurns,
    turnLimit: state.turnLimit,
    completionMode: state.completionMode,
    sessionCompleted: state.sessionCompleted,
    practiceExperience: state.practiceExperience,
    sceneCategory: state.sceneCategory,
    practiceMode: state.practiceMode,
    capabilities: state.capabilities,
    sessionVersion: state.sessionVersion,
    nextQuestion: state.currentQuestion,
    audioAssetId: turn.audioAssetId,
    speechFeedbackStatusUrl: turn.speechFeedbackStatusUrl,
  );
}

PracticeCapabilities _decodePracticeCapabilities(Map<String, Object?> value) {
  final root = _exactObject(
    value,
    required: const {
      'retry_allowed',
      'question_translation_allowed',
      'question_tips_allowed',
      'avatar_allowed',
      'speech_feedback_allowed',
    },
  );
  return PracticeCapabilities(
    retryAllowed: _boolean(root, 'retry_allowed'),
    questionTranslationAllowed: _boolean(root, 'question_translation_allowed'),
    questionTipsAllowed: _boolean(root, 'question_tips_allowed'),
    avatarAllowed: _boolean(root, 'avatar_allowed'),
    speechFeedbackAllowed: _boolean(root, 'speech_feedback_allowed'),
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
    optional: const {'question_type', 'parent_question_id'},
  );
  final addressees = _stringList(root, 'addressee_participant_ids');
  final questionType = root.containsKey('question_type')
      ? _string(root, 'question_type', maxLength: 16)
      : 'PRIMARY';
  final parentQuestionId = root.containsKey('parent_question_id')
      ? _string(root, 'parent_question_id')
      : null;
  if (addressees.isEmpty ||
      addressees.toSet().length != addressees.length ||
      (questionType != 'PRIMARY' && questionType != 'FOLLOW_UP') ||
      (questionType == 'PRIMARY' && parentQuestionId != null) ||
      (questionType == 'FOLLOW_UP' && parentQuestionId == null)) {
    throw _invalidResponse();
  }
  return PracticeQuestion(
    id: _string(root, 'question_id'),
    sessionId: _string(root, 'practice_session_id'),
    text: _string(root, 'content'),
    speakerParticipantId: _string(root, 'speaker_participant_id'),
    addresseeParticipantIds: addressees,
    speechPath: _string(root, 'speech_path'),
    questionType: questionType,
    parentQuestionId: parentQuestionId,
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
    optional: const {
      'audio_asset_id',
      'speech_feedback_status_url',
      'counts_toward_effective_turn_limit',
    },
  );
  if (root.containsKey('audio_asset_id') && root['audio_asset_id'] == null) {
    throw _invalidResponse();
  }
  final audioAssetId = root.containsKey('audio_asset_id')
      ? _string(root, 'audio_asset_id', maxLength: 128)
      : null;
  final speechFeedbackStatusUrl = root.containsKey('speech_feedback_status_url')
      ? _string(root, 'speech_feedback_status_url', maxLength: 160)
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
    countsTowardEffectiveTurnLimit:
        root.containsKey('counts_toward_effective_turn_limit')
        ? _boolean(root, 'counts_toward_effective_turn_limit')
        : true,
    audioAssetId: audioAssetId,
    speechFeedbackStatusUrl: speechFeedbackStatusUrl,
  );
  if (turn.evidenceVersion < 1 ||
      turn.effectiveTurns < 1 ||
      (speechFeedbackStatusUrl != null &&
          !validSpeechFeedbackStatusUrl(speechFeedbackStatusUrl))) {
    throw _invalidResponse();
  }
  return turn;
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

String _utf8String(
  Map<String, Object?> value,
  String key, {
  required int maxLength,
  required int maxBytes,
}) {
  final item = _string(value, key, maxLength: maxLength);
  if (utf8.encode(item).length > maxBytes) {
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

PracticeClientException _invalidResponse() {
  return const PracticeClientException(
    kind: PracticeClientFailureKind.invalidResponse,
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

String _requireRetryAnswerPath(String value) {
  final retryTurnId = _retryTurnIdInAnswerPath(value);
  if (retryTurnId == null) {
    throw ArgumentError.value(
      value,
      'answerPath',
      'Invalid server retry answer path.',
    );
  }
  return retryTurnId;
}

String _retryRequestIdFromStatusUrl(String value) {
  final match = _retryRequestStatusPathPattern.firstMatch(value);
  final id = match?.group(1);
  if (id == null || id == '.' || id == '..') {
    throw _invalidResponse();
  }
  return id;
}

String _retryTurnIdFromAnswerPath(String value) {
  final retryTurnId = _retryTurnIdInAnswerPath(value);
  if (retryTurnId == null) {
    throw _invalidResponse();
  }
  return retryTurnId;
}

String? _retryTurnIdInAnswerPath(String value) {
  final match = _retryAnswerPathPattern.firstMatch(value);
  final id = match?.group(1);
  return id == null || id == '.' || id == '..' ? null : id;
}
