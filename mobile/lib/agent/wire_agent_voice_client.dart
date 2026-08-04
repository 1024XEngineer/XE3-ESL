import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/bearer_authentication.dart';
import 'package:speakup/identity/network/transport_security.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';
import 'package:speakup/features/agent/handoff/agent_handoff_codec.dart';

import 'agent_client.dart';
import 'agent_models.dart';
import 'agent_voice_client.dart';
import 'agent_voice_models.dart';

final class AgentVoiceWireRequest {
  const AgentVoiceWireRequest({
    required this.method,
    required this.uri,
    required this.headers,
    required this.maximumResponseBytes,
    this.body,
  });

  final String method;
  final Uri uri;
  final Map<String, String> headers;
  final int maximumResponseBytes;
  final Uint8List? body;
}

final class AgentVoiceWireResponse {
  const AgentVoiceWireResponse({
    required this.statusCode,
    required this.body,
    this.headers = const <String, String>{},
  });

  final int statusCode;
  final Uint8List body;
  final Map<String, String> headers;
}

final class AgentVoiceWireStreamResponse {
  const AgentVoiceWireStreamResponse({
    required this.statusCode,
    required this.body,
    this.headers = const <String, String>{},
  });

  final int statusCode;
  final Stream<Uint8List> body;
  final Map<String, String> headers;
}

abstract interface class AgentVoiceWireTransport {
  Future<AgentVoiceWireResponse> send(AgentVoiceWireRequest request);

  Future<AgentVoiceWireStreamResponse> openStream(
    AgentVoiceWireRequest request,
  );

  void close({bool force = false});
}

typedef AgentVoiceWireTransportFactory = AgentVoiceWireTransport Function();
typedef AgentVoiceNow = DateTime Function();

final class WireAgentVoiceClient implements AgentVoiceClient {
  factory WireAgentVoiceClient({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    AgentVoiceWireTransport? apiTransport,
    AgentVoiceWireTransport? signedAudioTransport,
    AgentVoiceWireTransportFactory? transportFactory,
    AgentVoiceNow? now,
    Duration requestTimeout = const Duration(seconds: 75),
  }) {
    if (requestTimeout <= Duration.zero) {
      throw ArgumentError.value(
        requestTimeout,
        'requestTimeout',
        'must be positive',
      );
    }
    final createTransport =
        transportFactory ??
        () => IoAgentVoiceWireTransport(requestTimeout: requestTimeout);
    return WireAgentVoiceClient._(
      baseUri,
      TrustedIdentityHttpOrigin(baseUri),
      credentialProvider,
      invalidateSession,
      apiTransport ?? createTransport(),
      signedAudioTransport ?? createTransport(),
      apiTransport == null,
      signedAudioTransport == null,
      createTransport,
      requestTimeout,
      now ?? _utcNow,
    );
  }

  WireAgentVoiceClient._(
    this._baseUri,
    this._trustedOrigin,
    this._credentialProvider,
    this._invalidateSession,
    this._apiTransport,
    this._signedAudioTransport,
    this._ownsApiTransport,
    this._ownsSignedAudioTransport,
    this._transportFactory,
    this._requestTimeout,
    this._now,
  );

  static const _maximumJsonBytes = 1024 * 1024;
  static const _maximumAudioBytes = 7400000;
  static const _maximumPlaybackLifetime = Duration(minutes: 2);
  static const _maximumLocalClockSkew = Duration(seconds: 30);

  final Uri _baseUri;
  final TrustedIdentityHttpOrigin _trustedOrigin;
  final AuthSessionCredentialProvider _credentialProvider;
  final AuthSessionInvalidator _invalidateSession;
  AgentVoiceWireTransport _apiTransport;
  AgentVoiceWireTransport _signedAudioTransport;
  final bool _ownsApiTransport;
  final bool _ownsSignedAudioTransport;
  final AgentVoiceWireTransportFactory _transportFactory;
  final Duration _requestTimeout;
  final AgentVoiceNow _now;

  int _accountGeneration = 0;
  bool _disposed = false;
  Future<void>? _cleanupFuture;
  final Set<Future<void>> _inFlight = <Future<void>>{};

  @override
  Future<AgentVoiceCandidate> createCandidate({
    required String threadId,
    required AgentVoiceLocalRecording recording,
    required String idempotencyKey,
  }) async {
    AgentVoiceCandidate? completed;
    await for (final event in createCandidateStream(
      threadId: threadId,
      recording: recording,
      idempotencyKey: idempotencyKey,
    )) {
      if (event case AgentVoiceCandidateCompleted(:final candidate)) {
        completed = candidate;
      }
    }
    return completed ?? (throw _invalidResponse());
  }

  @override
  Stream<AgentVoiceTranscriptionEvent> createCandidateStream({
    required String threadId,
    required AgentVoiceLocalRecording recording,
    required String idempotencyKey,
  }) async* {
    _requireUuid(threadId);
    _requireClientIdentity(idempotencyKey, minimumLength: 8);
    if (recording.contentType != 'audio/wav' ||
        recording.sizeBytes < 45 ||
        recording.sizeBytes > _maximumAudioBytes ||
        recording.duration <= Duration.zero ||
        recording.duration > const Duration(seconds: 60)) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidRequest,
      );
    }
    final cleanup = _cleanupFuture;
    if (cleanup != null) {
      await cleanup;
    }
    if (_disposed) {
      throw const AgentClientOperationCancelled();
    }
    final generation = _accountGeneration;
    final completion = Completer<void>();
    final marker = completion.future;
    _inFlight.add(marker);
    try {
      final file = File(recording.path);
      late final Uint8List bytes;
      try {
        if (!await file.exists() ||
            await file.length() != recording.sizeBytes) {
          throw const AgentClientException(
            kind: AgentClientFailureKind.invalidRequest,
          );
        }
        bytes = await file.readAsBytes();
      } on AgentClientException {
        rethrow;
      } on FileSystemException {
        throw const AgentClientException(
          kind: AgentClientFailureKind.invalidRequest,
        );
      }
      try {
        if (!_isWave(bytes)) {
          throw const AgentClientException(
            kind: AgentClientFailureKind.invalidRequest,
          );
        }
        final response = await _openApiStream(
          generation: generation,
          method: 'POST',
          path:
              '/v1/agent-threads/${Uri.encodeComponent(threadId)}/voice-message-candidates/stream',
          accept: 'text/event-stream',
          contentType: 'audio/wav',
          headers: <String, String>{'Idempotency-Key': idempotencyKey},
          body: bytes,
          maximumResponseBytes: _maximumJsonBytes,
        );
        await for (final event in _decodeTranscriptionEvents(
          response,
          expectedThreadId: threadId,
        )) {
          _requireGeneration(generation);
          yield event;
        }
      } finally {
        _zero(bytes);
      }
    } on AgentClientException {
      rethrow;
    } on AgentClientOperationCancelled {
      rethrow;
    } on TimeoutException {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } catch (_) {
      throw const AgentClientException(kind: AgentClientFailureKind.unexpected);
    } finally {
      _inFlight.remove(marker);
      completion.complete();
    }
  }

  @override
  Future<AgentVoiceCandidate> getCandidate({required String candidateId}) {
    _requireUuid(candidateId);
    return _run((generation) async {
      final response = await _sendApi(
        generation: generation,
        method: 'GET',
        path:
            '/v1/agent-voice-message-candidates/${Uri.encodeComponent(candidateId)}',
        accept: ContentType.json.mimeType,
        maximumResponseBytes: _maximumJsonBytes,
      );
      try {
        _requireStatus(response, const <int>{HttpStatus.ok});
        final candidate = _decodeJson(response.body, _decodeCandidateObject);
        if (candidate.id != candidateId) {
          throw _invalidResponse();
        }
        return candidate;
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<AgentVoiceCandidate> retryCandidate({required String candidateId}) {
    _requireUuid(candidateId);
    return _run((generation) async {
      final response = await _sendApi(
        generation: generation,
        method: 'POST',
        path:
            '/v1/agent-voice-message-candidates/${Uri.encodeComponent(candidateId)}/retries',
        accept: ContentType.json.mimeType,
        maximumResponseBytes: _maximumJsonBytes,
      );
      try {
        _requireStatus(response, const <int>{HttpStatus.ok});
        final candidate = _decodeJson(response.body, _decodeCandidateObject);
        if (candidate.id != candidateId) {
          throw _invalidResponse();
        }
        return candidate;
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<void> deleteCandidate({required String candidateId}) {
    _requireUuid(candidateId);
    return _run((generation) async {
      final response = await _sendApi(
        generation: generation,
        method: 'DELETE',
        path:
            '/v1/agent-voice-message-candidates/${Uri.encodeComponent(candidateId)}',
        accept: ContentType.json.mimeType,
        maximumResponseBytes: _maximumJsonBytes,
      );
      try {
        _requireStatus(response, const <int>{HttpStatus.noContent});
        _requireEmpty(response);
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<AgentVoiceConfirmation> confirmCandidate({
    required String candidateId,
    required int candidateVersion,
    required String clientMessageId,
    required String confirmedText,
  }) {
    _requireUuid(candidateId);
    _requireClientIdentity(clientMessageId);
    _requireContent(confirmedText);
    if (candidateVersion < 1) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.invalidRequest,
      );
    }
    return _run((generation) async {
      final response = await _sendApi(
        generation: generation,
        method: 'POST',
        path:
            '/v1/agent-voice-message-candidates/${Uri.encodeComponent(candidateId)}/confirmations',
        accept: ContentType.json.mimeType,
        contentType: ContentType.json.mimeType,
        body: Uint8List.fromList(
          utf8.encode(
            jsonEncode(<String, Object?>{
              'candidate_version': candidateVersion,
              'client_message_id': clientMessageId,
              'confirmed_text': confirmedText,
            }),
          ),
        ),
        maximumResponseBytes: _maximumJsonBytes,
      );
      try {
        _requireStatus(response, const <int>{
          HttpStatus.created,
          HttpStatus.accepted,
        });
        final confirmation = _decodeJson(
          response.body,
          _decodeConfirmationObject,
        );
        if (confirmation.candidate.id != candidateId ||
            confirmation.candidate.version != candidateVersion ||
            confirmation.message.text != confirmedText ||
            confirmation.run.inputMessageId != confirmation.message.id ||
            confirmation.run.threadId != confirmation.candidate.threadId ||
            (response.statusCode == HttpStatus.created &&
                !confirmation.run.isTerminal) ||
            (response.statusCode == HttpStatus.accepted &&
                confirmation.run.isTerminal)) {
          throw _invalidResponse();
        }
        return confirmation;
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<AgentVoiceRun> getRun({required String runId}) {
    _requireUuid(runId);
    return _run((generation) async {
      final response = await _sendApi(
        generation: generation,
        method: 'GET',
        path: '/v1/agent-runs/${Uri.encodeComponent(runId)}',
        accept: ContentType.json.mimeType,
        maximumResponseBytes: _maximumJsonBytes,
      );
      try {
        _requireStatus(response, const <int>{HttpStatus.ok});
        final run = _decodeJson(response.body, _decodeRunObject);
        if (run.id != runId) {
          throw _invalidResponse();
        }
        return run;
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<AgentVoiceRun> retryRun({
    required String runId,
    required String clientRetryId,
  }) {
    _requireUuid(runId);
    _requireClientIdentity(clientRetryId);
    return _run((generation) async {
      final response = await _sendApi(
        generation: generation,
        method: 'POST',
        path: '/v1/agent-runs/${Uri.encodeComponent(runId)}/retries',
        accept: ContentType.json.mimeType,
        contentType: ContentType.json.mimeType,
        body: Uint8List.fromList(
          utf8.encode(
            jsonEncode(<String, Object?>{'client_retry_id': clientRetryId}),
          ),
        ),
        maximumResponseBytes: _maximumJsonBytes,
      );
      try {
        _requireStatus(response, const <int>{
          HttpStatus.created,
          HttpStatus.accepted,
        });
        final run = _decodeJson(response.body, _decodeRunObject);
        if ((response.statusCode == HttpStatus.created && !run.isTerminal) ||
            (response.statusCode == HttpStatus.accepted && run.isTerminal)) {
          throw _invalidResponse();
        }
        return run;
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<AgentMessage?> getMessage({
    required String threadId,
    required String messageId,
  }) {
    _requireUuid(threadId);
    _requireUuid(messageId);
    return _run((generation) async {
      final response = await _sendApi(
        generation: generation,
        method: 'GET',
        path: '/v1/agent-threads/${Uri.encodeComponent(threadId)}/messages',
        query: const <String, String>{'page_size': '100'},
        accept: ContentType.json.mimeType,
        maximumResponseBytes: _maximumJsonBytes,
      );
      try {
        _requireStatus(response, const <int>{HttpStatus.ok});
        final messages = _decodeJson(
          response.body,
          (value) => _decodeMessagePage(value, expectedThreadId: threadId),
        );
        return messages.where((message) => message.id == messageId).firstOrNull;
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<Uint8List> loadMessageAudio({required String audioId}) {
    _requireUuid(audioId);
    return _run((generation) async {
      final credential = _requireCredential();
      final metadata = await _sendApiWithCredential(
        generation: generation,
        credential: credential,
        method: 'GET',
        path:
            '/v1/agent-message-audios/${Uri.encodeComponent(audioId)}/playback',
        accept: ContentType.json.mimeType,
        maximumResponseBytes: _maximumJsonBytes,
      );
      late final _PlaybackCapability capability;
      try {
        _requireStatus(metadata, const <int>{HttpStatus.ok});
        if (_header(metadata.headers, HttpHeaders.cacheControlHeader) !=
            'no-store') {
          throw _invalidResponse();
        }
        capability = _decodeJson(
          metadata.body,
          (value) => _decodePlaybackObject(value, now: _now()),
        );
      } finally {
        _zero(metadata.body);
      }
      validateNoSessionCredentialInUri(
        capability.uri,
        sessionToken: credential.sessionToken,
      );
      _requireGeneration(generation);
      if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
        throw const AgentClientOperationCancelled();
      }
      final response = await _send(
        transport: _signedAudioTransport,
        request: AgentVoiceWireRequest(
          method: 'GET',
          uri: capability.uri,
          headers: const <String, String>{
            HttpHeaders.acceptHeader: 'audio/wav',
            HttpHeaders.cacheControlHeader: 'no-store',
          },
          maximumResponseBytes: _maximumAudioBytes,
        ),
      );
      try {
        _requireGeneration(generation);
        if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
          throw const AgentClientOperationCancelled();
        }
        _requireStatus(response, const <int>{HttpStatus.ok});
        _requireWave(response);
        return Uint8List.fromList(response.body);
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<void> deleteMessageAudio({required String audioId}) {
    _requireUuid(audioId);
    return _run((generation) async {
      final response = await _sendApi(
        generation: generation,
        method: 'DELETE',
        path: '/v1/agent-message-audios/${Uri.encodeComponent(audioId)}',
        accept: ContentType.json.mimeType,
        maximumResponseBytes: _maximumJsonBytes,
      );
      try {
        _requireStatus(response, const <int>{HttpStatus.noContent});
        _requireEmpty(response);
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<Uint8List> loadAssistantSpeech({required String messageId}) {
    _requireUuid(messageId);
    return _run((generation) async {
      final response = await _sendApi(
        generation: generation,
        method: 'GET',
        path: '/v1/agent-messages/${Uri.encodeComponent(messageId)}/speech',
        accept: 'audio/wav',
        maximumResponseBytes: _maximumAudioBytes,
      );
      try {
        _requireStatus(response, const <int>{HttpStatus.ok});
        if (_header(response.headers, HttpHeaders.cacheControlHeader) !=
            'no-store') {
          throw _invalidResponse();
        }
        _requireWave(response);
        return Uint8List.fromList(response.body);
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<Uint8List> loadSpeechPreview({
    required String messageId,
    required String text,
  }) {
    _requireUuid(messageId);
    return _run((generation) async {
      final response = await _sendApi(
        generation: generation,
        method: 'POST',
        path:
            '/v1/agent-messages/${Uri.encodeComponent(messageId)}/speech-previews',
        accept: 'audio/wav',
        contentType: ContentType.json.mimeType,
        body: utf8.encode(jsonEncode(<String, String>{'text': text})),
        maximumResponseBytes: _maximumAudioBytes,
      );
      try {
        _requireStatus(response, const <int>{HttpStatus.ok});
        _requireWave(response);
        return Uint8List.fromList(response.body);
      } finally {
        _zero(response.body);
      }
    });
  }

  @override
  Future<void> clearAccountState() {
    final existing = _cleanupFuture;
    if (existing != null) {
      return existing;
    }
    final cleanup = _performCleanup();
    _cleanupFuture = cleanup;
    return cleanup.whenComplete(() {
      if (identical(_cleanupFuture, cleanup)) {
        _cleanupFuture = null;
      }
    });
  }

  Future<void> _performCleanup() async {
    if (_disposed) {
      return;
    }
    final cleanupGeneration = ++_accountGeneration;
    if (_ownsApiTransport) {
      _apiTransport.close(force: true);
    }
    if (_ownsSignedAudioTransport) {
      _signedAudioTransport.close(force: true);
    }
    await Future.wait(List<Future<void>>.of(_inFlight));
    if (_disposed || cleanupGeneration != _accountGeneration) {
      return;
    }
    if (_ownsApiTransport) {
      _apiTransport = _transportFactory();
    }
    if (_ownsSignedAudioTransport) {
      _signedAudioTransport = _transportFactory();
    }
  }

  @override
  Future<void> dispose() async {
    if (_disposed) {
      return;
    }
    _disposed = true;
    _accountGeneration++;
    if (_ownsApiTransport) {
      _apiTransport.close(force: true);
    }
    if (_ownsSignedAudioTransport) {
      _signedAudioTransport.close(force: true);
    }
    await Future.wait(List<Future<void>>.of(_inFlight));
  }

  Future<AgentVoiceWireResponse> _sendApi({
    required int generation,
    required String method,
    required String path,
    required String accept,
    required int maximumResponseBytes,
    String? contentType,
    Map<String, String> headers = const <String, String>{},
    Map<String, String>? query,
    Uint8List? body,
  }) {
    return _sendApiWithCredential(
      generation: generation,
      credential: _requireCredential(),
      method: method,
      path: path,
      accept: accept,
      maximumResponseBytes: maximumResponseBytes,
      contentType: contentType,
      headers: headers,
      query: query,
      body: body,
    );
  }

  Future<AgentVoiceWireStreamResponse> _openApiStream({
    required int generation,
    required String method,
    required String path,
    required String accept,
    required int maximumResponseBytes,
    required String contentType,
    required Map<String, String> headers,
    required Uint8List body,
  }) async {
    _requireGeneration(generation);
    final credential = _requireCredential();
    final uri = _baseUri.resolve(path);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(
      uri,
      sessionToken: credential.sessionToken,
    );
    final response = await _openStream(
      AgentVoiceWireRequest(
        method: method,
        uri: uri,
        headers: <String, String>{
          HttpHeaders.acceptHeader: accept,
          HttpHeaders.authorizationHeader: bearerAuthorizationValue(
            credential.sessionToken,
          ),
          HttpHeaders.cacheControlHeader: 'no-store',
          HttpHeaders.contentTypeHeader: contentType,
          ...headers,
        },
        maximumResponseBytes: maximumResponseBytes,
        body: body,
      ),
    );
    _requireGeneration(generation);
    if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
      throw const AgentClientOperationCancelled();
    }
    if (response.statusCode != HttpStatus.ok) {
      final responseBody = await _readStreamBody(
        response.body,
        maximumResponseBytes,
      );
      final buffered = AgentVoiceWireResponse(
        statusCode: response.statusCode,
        body: responseBody,
        headers: response.headers,
      );
      if (response.statusCode == HttpStatus.unauthorized) {
        unawaited(_invalidateCredential(credential));
      }
      try {
        throw _exceptionFor(buffered);
      } finally {
        _zero(responseBody);
      }
    }
    final responseType = _header(
      response.headers,
      HttpHeaders.contentTypeHeader,
    )?.split(';').first.trim().toLowerCase();
    if (responseType != 'text/event-stream') {
      throw _invalidResponse();
    }
    return response;
  }

  Future<AgentVoiceWireStreamResponse> _openStream(
    AgentVoiceWireRequest request,
  ) async {
    try {
      return await _apiTransport.openStream(request);
    } on AgentClientException {
      rethrow;
    } on TimeoutException {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    }
  }

  Stream<AgentVoiceTranscriptionEvent> _decodeTranscriptionEvents(
    AgentVoiceWireStreamResponse response, {
    required String expectedThreadId,
  }) async* {
    var responseBytes = 0;
    var eventName = '';
    var eventData = '';
    var started = false;
    var completed = false;
    try {
      final lines = response.body
          .map((chunk) {
            responseBytes += chunk.length;
            if (responseBytes > _maximumJsonBytes) {
              throw _invalidResponse();
            }
            return chunk;
          })
          .cast<List<int>>()
          .transform(utf8.decoder)
          .transform(const LineSplitter());
      await for (final line in lines.timeout(_requestTimeout)) {
        if (line.isEmpty) {
          if (eventName.isEmpty && eventData.isEmpty) {
            continue;
          }
          if (completed) {
            throw _invalidResponse();
          }
          final decoded = _decodeTranscriptionEvent(
            eventName,
            eventData,
            expectedThreadId: expectedThreadId,
          );
          if (eventName == 'transcription.started') {
            if (started || decoded != null) {
              throw _invalidResponse();
            }
            started = true;
          } else {
            if (!started || decoded == null) {
              throw _invalidResponse();
            }
            if (decoded is AgentVoiceCandidateCompleted) {
              completed = true;
            }
            yield decoded;
          }
          eventName = '';
          eventData = '';
          continue;
        }
        if (line.startsWith(':')) {
          continue;
        }
        if (line.startsWith('event: ')) {
          if (eventName.isNotEmpty) {
            throw _invalidResponse();
          }
          eventName = line.substring(7);
          continue;
        }
        if (line.startsWith('data: ')) {
          if (eventData.isNotEmpty) {
            throw _invalidResponse();
          }
          eventData = line.substring(6);
          continue;
        }
        throw _invalidResponse();
      }
    } on _InvalidVoiceResponse {
      throw _invalidResponse();
    }
    if (!started ||
        !completed ||
        eventName.isNotEmpty ||
        eventData.isNotEmpty) {
      throw _invalidResponse();
    }
  }

  AgentVoiceTranscriptionEvent? _decodeTranscriptionEvent(
    String event,
    String data, {
    required String expectedThreadId,
  }) {
    final Object? decoded;
    try {
      decoded = jsonDecode(data);
    } catch (_) {
      throw const _InvalidVoiceResponse();
    }
    switch (event) {
      case 'transcription.started':
        _strictObject(
          decoded,
          allowed: const <String>{},
          required: const <String>{},
        );
        return null;
      case 'transcription.updated':
        final update = _strictObject(
          decoded,
          allowed: const <String>{'transcript', 'final'},
          required: const <String>{'transcript', 'final'},
        );
        return AgentVoiceTranscriptUpdated(
          text: _strictContent(update['transcript']),
          finalResult: _strictBool(update['final']),
        );
      case 'candidate.ready':
      case 'candidate.failed':
        final envelope = _strictObject(
          decoded,
          allowed: const <String>{'candidate', 'kind', 'retryable'},
          required: event == 'candidate.ready'
              ? const <String>{'candidate'}
              : const <String>{},
        );
        if (envelope['candidate'] case final candidateValue?) {
          if (envelope.length != 1) {
            throw const _InvalidVoiceResponse();
          }
          final candidate = _decodeCandidateObject(candidateValue);
          if (candidate.threadId != expectedThreadId ||
              (event == 'candidate.ready' && !candidate.isReady) ||
              (event == 'candidate.failed' &&
                  candidate.status != AgentVoiceCandidateStatus.failed)) {
            throw const _InvalidVoiceResponse();
          }
          return AgentVoiceCandidateCompleted(candidate);
        }
        _requireOnly(
          envelope,
          allowed: const <String>{'kind', 'retryable'},
          required: const <String>{'kind', 'retryable'},
        );
        throw AgentClientException(
          kind: AgentClientFailureKind.server,
          errorCode: _strictString(envelope['kind'], min: 1, max: 64),
          retryable: _strictBool(envelope['retryable']),
        );
      default:
        throw const _InvalidVoiceResponse();
    }
  }

  Future<AgentVoiceWireResponse> _sendApiWithCredential({
    required int generation,
    required AuthSessionCredential credential,
    required String method,
    required String path,
    required String accept,
    required int maximumResponseBytes,
    String? contentType,
    Map<String, String> headers = const <String, String>{},
    Map<String, String>? query,
    Uint8List? body,
  }) async {
    _requireGeneration(generation);
    final resolved = _baseUri.resolve(path);
    final uri = query == null || query.isEmpty
        ? resolved
        : resolved.replace(queryParameters: query);
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(
      uri,
      sessionToken: credential.sessionToken,
    );
    final response = await _send(
      transport: _apiTransport,
      request: AgentVoiceWireRequest(
        method: method,
        uri: uri,
        headers: <String, String>{
          HttpHeaders.acceptHeader: accept,
          HttpHeaders.authorizationHeader: bearerAuthorizationValue(
            credential.sessionToken,
          ),
          HttpHeaders.cacheControlHeader: 'no-store',
          HttpHeaders.contentTypeHeader: ?contentType,
          ...headers,
        },
        maximumResponseBytes: maximumResponseBytes,
        body: body,
      ),
    );
    _requireGeneration(generation);
    if (!isSameAuthSessionCredential(_credentialProvider(), credential)) {
      _zero(response.body);
      throw const AgentClientOperationCancelled();
    }
    if (response.statusCode == HttpStatus.unauthorized) {
      unawaited(_invalidateCredential(credential));
      final exception = _exceptionFor(response);
      _zero(response.body);
      throw exception;
    }
    return response;
  }

  Future<AgentVoiceWireResponse> _send({
    required AgentVoiceWireTransport transport,
    required AgentVoiceWireRequest request,
  }) async {
    try {
      return await transport.send(request);
    } on AgentClientException {
      rethrow;
    } on TimeoutException {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on SocketException {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on HttpException {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } on IOException {
      throw const AgentClientException(
        kind: AgentClientFailureKind.network,
        retryable: true,
      );
    } catch (_) {
      throw const AgentClientException(kind: AgentClientFailureKind.unexpected);
    }
  }

  Future<T> _run<T>(Future<T> Function(int generation) action) async {
    final cleanup = _cleanupFuture;
    if (cleanup != null) {
      await cleanup;
    }
    if (_disposed) {
      throw const AgentClientOperationCancelled();
    }
    final generation = _accountGeneration;
    final completion = Completer<void>();
    final marker = completion.future;
    _inFlight.add(marker);
    try {
      return await action(generation);
    } finally {
      _inFlight.remove(marker);
      completion.complete();
    }
  }

  AuthSessionCredential _requireCredential() {
    final credential = _credentialProvider();
    if (credential == null) {
      throw const AgentClientException(
        kind: AgentClientFailureKind.authenticationRequired,
        statusCode: HttpStatus.unauthorized,
        errorCode: 'authentication_required',
      );
    }
    return credential;
  }

  void _requireGeneration(int generation) {
    if (_disposed || generation != _accountGeneration) {
      throw const AgentClientOperationCancelled();
    }
  }

  Future<void> _invalidateCredential(AuthSessionCredential credential) async {
    try {
      await _invalidateSession(
        expectedSessionToken: credential.sessionToken,
        expectedGeneration: credential.generation,
      );
    } catch (_) {
      // The request remains failed closed; auth cleanup may retry.
    }
  }

  void _requireStatus(AgentVoiceWireResponse response, Set<int> expected) {
    if (!expected.contains(response.statusCode)) {
      throw _exceptionFor(response);
    }
  }

  void _requireEmpty(AgentVoiceWireResponse response) {
    if (response.body.isNotEmpty) {
      throw _invalidResponse();
    }
  }

  void _requireWave(AgentVoiceWireResponse response) {
    final contentType = _header(
      response.headers,
      HttpHeaders.contentTypeHeader,
    )?.split(';').first.trim().toLowerCase();
    if (contentType != 'audio/wav' ||
        response.body.length < 12 ||
        response.body.length > _maximumAudioBytes ||
        !_isWave(response.body)) {
      throw _invalidResponse();
    }
  }

  AgentClientException _exceptionFor(AgentVoiceWireResponse response) {
    String? code;
    String? correlationId;
    bool retryable = false;
    try {
      final root = _decodeJson(
        response.body,
        (value) => _strictObject(
          value,
          allowed: const <String>{'error'},
          required: const <String>{'error'},
        ),
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
      code = _strictString(error['code'], min: 1, max: 64);
      _strictString(error['message'], min: 1, max: 512);
      retryable = _strictBool(error['retryable']);
      correlationId = _strictString(error['correlation_id'], min: 1, max: 128);
    } catch (_) {
      code = null;
      correlationId = null;
    }
    final kind = switch (response.statusCode) {
      HttpStatus.badRequest => AgentClientFailureKind.invalidRequest,
      HttpStatus.unauthorized => AgentClientFailureKind.authenticationRequired,
      HttpStatus.notFound => AgentClientFailureKind.notFound,
      HttpStatus.conflict => AgentClientFailureKind.conflict,
      HttpStatus.tooManyRequests => AgentClientFailureKind.rateLimited,
      >= 500 => AgentClientFailureKind.server,
      _ => AgentClientFailureKind.unexpected,
    };
    return AgentClientException(
      kind: kind,
      statusCode: response.statusCode,
      errorCode: _normalizedErrorCode(response.statusCode, code),
      retryable:
          retryable ||
          kind == AgentClientFailureKind.rateLimited ||
          kind == AgentClientFailureKind.server,
      correlationId: correlationId,
    );
  }
}

final class IoAgentVoiceWireTransport implements AgentVoiceWireTransport {
  IoAgentVoiceWireTransport({required Duration requestTimeout})
    : _requestTimeout = requestTimeout,
      _client = HttpClient()..connectionTimeout = requestTimeout;

  final Duration _requestTimeout;
  final HttpClient _client;

  @override
  Future<AgentVoiceWireResponse> send(AgentVoiceWireRequest request) async {
    HttpClientRequest? nativeRequest;
    try {
      nativeRequest = await _client
          .openUrl(request.method, request.uri)
          .timeout(_requestTimeout);
      nativeRequest.followRedirects = false;
      request.headers.forEach(nativeRequest.headers.set);
      if (request.body case final body?) {
        nativeRequest.add(body);
      }
      final response = await nativeRequest.close().timeout(
        _requestTimeout,
        onTimeout: () {
          nativeRequest?.abort();
          throw TimeoutException('Agent voice response timed out.');
        },
      );
      if (response.contentLength > request.maximumResponseBytes) {
        nativeRequest.abort();
        throw _invalidResponse();
      }
      final builder = BytesBuilder(copy: false);
      var length = 0;
      await for (final chunk in response.timeout(_requestTimeout)) {
        length += chunk.length;
        if (length > request.maximumResponseBytes) {
          nativeRequest.abort();
          throw _invalidResponse();
        }
        builder.add(chunk);
      }
      final headers = <String, String>{};
      response.headers.forEach((name, values) {
        headers[name.toLowerCase()] = values.join(',');
      });
      return AgentVoiceWireResponse(
        statusCode: response.statusCode,
        body: builder.takeBytes(),
        headers: headers,
      );
    } on TimeoutException {
      nativeRequest?.abort();
      rethrow;
    }
  }

  @override
  Future<AgentVoiceWireStreamResponse> openStream(
    AgentVoiceWireRequest request,
  ) async {
    HttpClientRequest? nativeRequest;
    try {
      nativeRequest = await _client
          .openUrl(request.method, request.uri)
          .timeout(_requestTimeout);
      nativeRequest.followRedirects = false;
      request.headers.forEach(nativeRequest.headers.set);
      if (request.body case final body?) {
        nativeRequest.contentLength = body.length;
        nativeRequest.add(body);
      }
      final response = await nativeRequest.close().timeout(
        _requestTimeout,
        onTimeout: () {
          nativeRequest?.abort();
          throw TimeoutException('Agent voice stream timed out.');
        },
      );
      final headers = <String, String>{};
      response.headers.forEach((name, values) {
        headers[name.toLowerCase()] = values.join(',');
      });
      return AgentVoiceWireStreamResponse(
        statusCode: response.statusCode,
        body: response.map(Uint8List.fromList),
        headers: headers,
      );
    } on TimeoutException {
      nativeRequest?.abort();
      rethrow;
    }
  }

  @override
  void close({bool force = false}) {
    _client.close(force: force);
  }
}

Future<Uint8List> _readStreamBody(
  Stream<Uint8List> stream,
  int maximumBytes,
) async {
  final builder = BytesBuilder(copy: false);
  var length = 0;
  await for (final chunk in stream) {
    length += chunk.length;
    if (length > maximumBytes) {
      throw _invalidResponse();
    }
    builder.add(chunk);
  }
  return builder.takeBytes();
}

AgentVoiceCandidate _decodeCandidateObject(Object? value) {
  final object = _strictObject(
    value,
    allowed: const <String>{
      'candidate_id',
      'thread_id',
      'status',
      'asr_attempt',
      'candidate_version',
      'recording',
      'transcript',
      'failure',
      'expires_at',
      'confirmed_message_id',
      'confirmed_run_id',
      'message_audio_id',
      'confirmed_at',
      'deleted_at',
      'created_at',
      'updated_at',
    },
    required: const <String>{
      'candidate_id',
      'thread_id',
      'status',
      'asr_attempt',
      'candidate_version',
      'recording',
      'expires_at',
      'created_at',
      'updated_at',
    },
  );
  final status = switch (_strictString(object['status'], min: 1, max: 24)) {
    'staged' => AgentVoiceCandidateStatus.staged,
    'transcribing' => AgentVoiceCandidateStatus.transcribing,
    'candidate_ready' => AgentVoiceCandidateStatus.candidateReady,
    'failed' => AgentVoiceCandidateStatus.failed,
    'confirming' => AgentVoiceCandidateStatus.confirming,
    'confirmed' => AgentVoiceCandidateStatus.confirmed,
    'deleting' => AgentVoiceCandidateStatus.deleting,
    'deleted' => AgentVoiceCandidateStatus.deleted,
    _ => throw const _InvalidVoiceResponse(),
  };
  final recordingObject = _strictObject(
    object['recording'],
    allowed: const <String>{
      'content_type',
      'size_bytes',
      'duration_ms',
      'sample_rate',
    },
    required: const <String>{
      'content_type',
      'size_bytes',
      'duration_ms',
      'sample_rate',
    },
  );
  if (_strictString(recordingObject['content_type'], min: 1, max: 32) !=
      'audio/wav') {
    throw const _InvalidVoiceResponse();
  }
  final recording = AgentVoiceRecordingMetadata(
    contentType: 'audio/wav',
    sizeBytes: _strictInt(recordingObject['size_bytes'], min: 1, max: 7400000),
    duration: Duration(
      milliseconds: _strictInt(
        recordingObject['duration_ms'],
        min: 1,
        max: 60000,
      ),
    ),
    sampleRate: _strictInt(
      recordingObject['sample_rate'],
      min: 8000,
      max: 48000,
    ),
  );
  final transcriptObject = _optionalObject(object, 'transcript');
  final transcript = transcriptObject == null
      ? null
      : AgentVoiceTranscript(
          text: _strictContent(transcriptObject['candidate_text']),
          requestId: _strictString(
            transcriptObject['request_id'],
            min: 1,
            max: 128,
          ),
          provider: _strictPattern(
            transcriptObject['provider'],
            _providerPattern,
            64,
          ),
          model: _strictPattern(
            transcriptObject['model'],
            _clientIdentityPattern,
            128,
          ),
          language: _optionalString(transcriptObject, 'language', max: 64),
          emotion: _optionalString(transcriptObject, 'emotion', max: 64),
          finishReason: _optionalString(
            transcriptObject,
            'finish_reason',
            max: 64,
          ),
        );
  if (transcriptObject != null) {
    _requireOnly(
      transcriptObject,
      allowed: const <String>{
        'candidate_text',
        'request_id',
        'provider',
        'model',
        'language',
        'emotion',
        'finish_reason',
      },
      required: const <String>{
        'candidate_text',
        'request_id',
        'provider',
        'model',
      },
    );
  }
  final failureObject = _optionalObject(object, 'failure');
  final failure = failureObject == null
      ? null
      : AgentVoiceCandidateFailure(
          kind: _strictPattern(failureObject['kind'], _failurePattern, 64),
          retryable: _strictBool(failureObject['retryable']),
        );
  if (failureObject != null) {
    _requireOnly(
      failureObject,
      allowed: const <String>{'kind', 'retryable'},
      required: const <String>{'kind', 'retryable'},
    );
  }
  final createdAt = _strictDateTime(object['created_at']);
  final updatedAt = _strictDateTime(object['updated_at']);
  final expiresAt = _strictDateTime(object['expires_at']);
  final candidate = AgentVoiceCandidate(
    id: _strictUuid(object['candidate_id']),
    threadId: _strictUuid(object['thread_id']),
    status: status,
    asrAttempt: _strictInt(object['asr_attempt'], min: 0),
    version: _strictInt(object['candidate_version'], min: 0),
    recording: recording,
    transcript: transcript,
    failure: failure,
    expiresAt: expiresAt,
    confirmedMessageId: _optionalUuid(object, 'confirmed_message_id'),
    confirmedRunId: _optionalUuid(object, 'confirmed_run_id'),
    messageAudioId: _optionalUuid(object, 'message_audio_id'),
    confirmedAt: _optionalDateTime(object, 'confirmed_at'),
    deletedAt: _optionalDateTime(object, 'deleted_at'),
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
  final confirmationFields = <Object?>[
    candidate.confirmedMessageId,
    candidate.confirmedRunId,
    candidate.messageAudioId,
    candidate.confirmedAt,
  ];
  final statusHasConfirmation =
      status == AgentVoiceCandidateStatus.confirmed ||
      status == AgentVoiceCandidateStatus.deleting ||
      status == AgentVoiceCandidateStatus.deleted;
  final hasAnyConfirmation = confirmationFields.any((field) => field != null);
  final hasAllConfirmation = confirmationFields.every((field) => field != null);
  if (updatedAt.isBefore(createdAt) ||
      expiresAt.isBefore(createdAt) ||
      (status == AgentVoiceCandidateStatus.candidateReady &&
          (transcript == null || failure != null || candidate.version < 1)) ||
      (status == AgentVoiceCandidateStatus.failed &&
          (transcript != null || failure == null)) ||
      (hasAnyConfirmation && !hasAllConfirmation) ||
      (status == AgentVoiceCandidateStatus.confirmed && !hasAllConfirmation) ||
      (!statusHasConfirmation && hasAnyConfirmation)) {
    throw const _InvalidVoiceResponse();
  }
  return candidate;
}

AgentVoiceConfirmation _decodeConfirmationObject(Object? value) {
  final object = _strictObject(
    value,
    allowed: const <String>{'candidate', 'message', 'run'},
    required: const <String>{'candidate', 'message', 'run'},
  );
  final candidate = _decodeCandidateObject(object['candidate']);
  final message = _decodeMessageObject(
    object['message'],
    expectedThreadId: candidate.threadId,
  );
  return AgentVoiceConfirmation(
    candidate: candidate,
    message: message,
    run: _decodeRunObject(object['run']),
  );
}

AgentVoiceRun _decodeRunObject(Object? value) {
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
  _strictInt(object['attempt'], min: 1);
  _strictPattern(object['requested_provider'], _providerPattern, 64);
  _strictPattern(object['requested_model'], _clientIdentityPattern, 128);
  _strictInt(object['max_output_tokens'], min: 1);
  final createdAt = _strictDateTime(object['created_at']);
  final updatedAt = _strictDateTime(object['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const _InvalidVoiceResponse();
  }
  final retryOf = _optionalUuid(object, 'retry_of_run_id');
  final retryId = _optionalPattern(
    object,
    'client_retry_id',
    _clientIdentityPattern,
    128,
  );
  if ((retryOf == null) != (retryId == null)) {
    throw const _InvalidVoiceResponse();
  }
  final status = switch (_strictString(object['status'], min: 1, max: 16)) {
    'pending' => AgentVoiceRunStatus.pending,
    'running' => AgentVoiceRunStatus.running,
    'completed' => AgentVoiceRunStatus.completed,
    'failed' => AgentVoiceRunStatus.failed,
    _ => throw const _InvalidVoiceResponse(),
  };
  final assistantId = _optionalUuid(object, 'assistant_message_id');
  final failureObject = _optionalObject(object, 'failure');
  final failureKind = failureObject == null
      ? null
      : _strictPattern(failureObject['kind'], _failurePattern, 64);
  final failureRetryable = failureObject == null
      ? false
      : _strictBool(failureObject['retryable']);
  if (failureObject != null) {
    _requireOnly(
      failureObject,
      allowed: const <String>{'kind', 'retryable'},
      required: const <String>{'kind', 'retryable'},
    );
  }
  final startedAt = _optionalDateTime(object, 'started_at');
  final completedAt = _optionalDateTime(object, 'completed_at');
  switch (status) {
    case AgentVoiceRunStatus.pending:
      if (assistantId != null ||
          failureObject != null ||
          startedAt != null ||
          completedAt != null) {
        throw const _InvalidVoiceResponse();
      }
    case AgentVoiceRunStatus.running:
      if (assistantId != null ||
          failureObject != null ||
          startedAt == null ||
          completedAt != null) {
        throw const _InvalidVoiceResponse();
      }
    case AgentVoiceRunStatus.completed:
      if (assistantId == null ||
          failureObject != null ||
          startedAt == null ||
          completedAt == null ||
          !object.containsKey('provider_completion_id') ||
          !object.containsKey('provider_model') ||
          !object.containsKey('finish_reason') ||
          !object.containsKey('usage')) {
        throw const _InvalidVoiceResponse();
      }
      _strictPattern(
        object['provider_completion_id'],
        _clientIdentityPattern,
        128,
      );
      _strictPattern(object['provider_model'], _clientIdentityPattern, 128);
      final finishReason = _strictString(
        object['finish_reason'],
        min: 1,
        max: 16,
      );
      if (finishReason != 'stop' && finishReason != 'length') {
        throw const _InvalidVoiceResponse();
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
      _strictInt(usage['input_tokens'], min: 0);
      _strictInt(usage['output_tokens'], min: 0);
      _strictInt(usage['total_tokens'], min: 0);
    case AgentVoiceRunStatus.failed:
      if (assistantId != null ||
          failureObject == null ||
          startedAt == null ||
          completedAt == null ||
          object.containsKey('usage')) {
        throw const _InvalidVoiceResponse();
      }
  }
  if (startedAt != null && startedAt.isBefore(createdAt) ||
      completedAt != null && completedAt.isBefore(startedAt!)) {
    throw const _InvalidVoiceResponse();
  }
  return AgentVoiceRun(
    id: _strictUuid(object['run_id']),
    threadId: _strictUuid(object['thread_id']),
    inputMessageId: _strictUuid(object['input_message_id']),
    status: status,
    assistantMessageId: assistantId,
    failureKind: failureKind,
    failureRetryable: failureRetryable,
  );
}

List<AgentMessage> _decodeMessagePage(
  Object? value, {
  required String expectedThreadId,
}) {
  final object = _strictObject(
    value,
    allowed: const <String>{'messages', 'next_cursor'},
    required: const <String>{'messages'},
  );
  final values = _strictList(object['messages'], max: 100);
  final messages = <AgentMessage>[];
  final ids = <String>{};
  var previousSequence = 0;
  for (final value in values) {
    final message = _decodeMessageObject(
      value,
      expectedThreadId: expectedThreadId,
    );
    final sequence = message.sequence!;
    if (!ids.add(message.id) || sequence <= previousSequence) {
      throw const _InvalidVoiceResponse();
    }
    previousSequence = sequence;
    messages.add(message);
  }
  _optionalString(object, 'next_cursor', max: 1024);
  return messages;
}

AgentMessage _decodeMessageObject(
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
      'handoffs',
      'speech_feedback_status_url',
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
  if (_strictUuid(object['thread_id']) != expectedThreadId) {
    throw const _InvalidVoiceResponse();
  }
  final role = switch (_strictString(object['role'], min: 1, max: 16)) {
    'user' => AgentMessageRole.user,
    'assistant' => AgentMessageRole.assistant,
    _ => throw const _InvalidVoiceResponse(),
  };
  final clientId = _optionalPattern(
    object,
    'client_message_id',
    _clientIdentityPattern,
    128,
  );
  final producedBy = _optionalUuid(object, 'produced_by_run_id');
  final modalityValue = _optionalString(object, 'modality', max: 16);
  final modality = switch (modalityValue) {
    null => AgentMessageModality.text,
    'voice' => AgentMessageModality.voice,
    _ => throw const _InvalidVoiceResponse(),
  };
  final audioObject = _optionalObject(object, 'audio');
  final audio = audioObject == null ? null : _decodeMessageAudio(audioObject);
  final speechFeedbackStatusUrl = _optionalString(
    object,
    'speech_feedback_status_url',
    max: 160,
  );
  final handoffs = object['handoffs'] == null
      ? const <AgentHandoff>[]
      : decodeAgentHandoffs(object['handoffs']);
  if ((role == AgentMessageRole.user &&
          (clientId == null || producedBy != null || handoffs.isNotEmpty)) ||
      (role == AgentMessageRole.assistant &&
          (clientId != null || producedBy == null)) ||
      (modality == AgentMessageModality.voice &&
          (role != AgentMessageRole.user || audio == null)) ||
      (modality == AgentMessageModality.text && audio != null) ||
      (speechFeedbackStatusUrl != null &&
          (role != AgentMessageRole.user ||
              modality != AgentMessageModality.voice ||
              !validSpeechFeedbackStatusUrl(speechFeedbackStatusUrl)))) {
    throw const _InvalidVoiceResponse();
  }
  return AgentMessage(
    id: _strictUuid(object['message_id']),
    role: role,
    text: _strictContent(object['content']),
    sequence: _strictInt(object['sequence'], min: 1),
    createdAt: _strictDateTime(object['created_at']),
    modality: modality,
    audio: audio,
    handoffs: handoffs,
    speechFeedbackStatusUrl: speechFeedbackStatusUrl,
  );
}

AgentMessageAudio _decodeMessageAudio(Map<String, Object?> object) {
  _requireOnly(
    object,
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
  if (_strictString(object['content_type'], min: 1, max: 32) != 'audio/wav') {
    throw const _InvalidVoiceResponse();
  }
  final id = _strictUuid(object['audio_id']);
  final status = switch (_strictString(object['status'], min: 1, max: 16)) {
    'readable' => AgentMessageAudioStatus.readable,
    'deleting' => AgentMessageAudioStatus.deleting,
    'deleted' => AgentMessageAudioStatus.deleted,
    _ => throw const _InvalidVoiceResponse(),
  };
  final playback = _optionalPattern(
    object,
    'playback_path',
    _playbackPathPattern,
    256,
  );
  final deletedAt = _optionalDateTime(object, 'deleted_at');
  if ((status == AgentMessageAudioStatus.readable &&
          (playback == null || deletedAt != null)) ||
      (status == AgentMessageAudioStatus.deleting && playback != null) ||
      (status == AgentMessageAudioStatus.deleted &&
          (playback != null || deletedAt == null)) ||
      (playback != null &&
          playback != '/v1/agent-message-audios/$id/playback')) {
    throw const _InvalidVoiceResponse();
  }
  return AgentMessageAudio(
    id: id,
    status: status,
    contentType: 'audio/wav',
    sizeBytes: _strictInt(object['size_bytes'], min: 1, max: 7400000),
    duration: Duration(
      milliseconds: _strictInt(object['duration_ms'], min: 1, max: 60000),
    ),
    playbackPath: playback,
    deletedAt: deletedAt,
  );
}

_PlaybackCapability _decodePlaybackObject(
  Object? value, {
  required DateTime now,
}) {
  final object = _strictObject(
    value,
    allowed: const <String>{'playback_url', 'expires_at'},
    required: const <String>{'playback_url', 'expires_at'},
  );
  final raw = _strictString(object['playback_url'], min: 1, max: 4096);
  final uri = Uri.tryParse(raw);
  final expiresAt = _strictDateTime(object['expires_at']);
  final utcNow = now.toUtc();
  if (uri == null ||
      uri.scheme != 'https' ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      uri.fragment.isNotEmpty ||
      !expiresAt.isAfter(
        utcNow.subtract(WireAgentVoiceClient._maximumLocalClockSkew),
      ) ||
      expiresAt.isAfter(
        utcNow.add(
          WireAgentVoiceClient._maximumPlaybackLifetime +
              WireAgentVoiceClient._maximumLocalClockSkew,
        ),
      )) {
    throw const _InvalidVoiceResponse();
  }
  return _PlaybackCapability(uri: uri, expiresAt: expiresAt);
}

final class _PlaybackCapability {
  const _PlaybackCapability({required this.uri, required this.expiresAt});

  final Uri uri;
  final DateTime expiresAt;
}

T _decodeJson<T>(Uint8List body, T Function(Object? value) decode) {
  try {
    return decode(jsonDecode(utf8.decode(body)));
  } catch (error) {
    if (error is AgentClientException) {
      rethrow;
    }
    throw _invalidResponse();
  }
}

Map<String, Object?> _strictObject(
  Object? value, {
  required Set<String> allowed,
  required Set<String> required,
}) {
  if (value is! Map) {
    throw const _InvalidVoiceResponse();
  }
  final result = <String, Object?>{};
  for (final entry in value.entries) {
    if (entry.key is! String ||
        !allowed.contains(entry.key) ||
        result.containsKey(entry.key)) {
      throw const _InvalidVoiceResponse();
    }
    result[entry.key as String] = entry.value;
  }
  if (!result.keys.toSet().containsAll(required)) {
    throw const _InvalidVoiceResponse();
  }
  return result;
}

void _requireOnly(
  Map<String, Object?> value, {
  required Set<String> allowed,
  required Set<String> required,
}) {
  if (value.keys.any((key) => !allowed.contains(key)) ||
      !value.keys.toSet().containsAll(required)) {
    throw const _InvalidVoiceResponse();
  }
}

List<Object?> _strictList(Object? value, {required int max}) {
  if (value is! List || value.length > max) {
    throw const _InvalidVoiceResponse();
  }
  return List<Object?>.of(value);
}

String _strictString(Object? value, {required int min, required int max}) {
  if (value is! String ||
      value.runes.length < min ||
      value.runes.length > max) {
    throw const _InvalidVoiceResponse();
  }
  return value;
}

String _strictPattern(Object? value, RegExp pattern, int max) {
  final result = _strictString(value, min: 1, max: max);
  if (!pattern.hasMatch(result)) {
    throw const _InvalidVoiceResponse();
  }
  return result;
}

String _strictUuid(Object? value) {
  return _strictPattern(value, _uuidPattern, 36);
}

String _strictContent(Object? value) {
  final content = _strictString(value, min: 1, max: 4096);
  if (content.trim().isEmpty || utf8.encode(content).length > 16384) {
    throw const _InvalidVoiceResponse();
  }
  return content;
}

int _strictInt(Object? value, {required int min, int? max}) {
  if (value is! int || value < min || (max != null && value > max)) {
    throw const _InvalidVoiceResponse();
  }
  return value;
}

bool _strictBool(Object? value) {
  if (value is! bool) {
    throw const _InvalidVoiceResponse();
  }
  return value;
}

DateTime _strictDateTime(Object? value) {
  final raw = _strictString(value, min: 1, max: 64);
  final parsed = DateTime.tryParse(raw);
  if (parsed == null || !raw.contains(RegExp(r'(Z|[+-]\d{2}:\d{2})$'))) {
    throw const _InvalidVoiceResponse();
  }
  return parsed.toUtc();
}

Map<String, Object?>? _optionalObject(Map<String, Object?> object, String key) {
  if (!object.containsKey(key)) {
    return null;
  }
  if (object[key] is! Map) {
    throw const _InvalidVoiceResponse();
  }
  return Map<String, Object?>.from(object[key]! as Map);
}

String? _optionalString(
  Map<String, Object?> object,
  String key, {
  required int max,
}) {
  if (!object.containsKey(key)) {
    return null;
  }
  return _strictString(object[key], min: 1, max: max);
}

String? _optionalPattern(
  Map<String, Object?> object,
  String key,
  RegExp pattern,
  int max,
) {
  if (!object.containsKey(key)) {
    return null;
  }
  return _strictPattern(object[key], pattern, max);
}

String? _optionalUuid(Map<String, Object?> object, String key) {
  if (!object.containsKey(key)) {
    return null;
  }
  return _strictUuid(object[key]);
}

DateTime? _optionalDateTime(Map<String, Object?> object, String key) {
  if (!object.containsKey(key)) {
    return null;
  }
  return _strictDateTime(object[key]);
}

void _requireUuid(String value) {
  if (!_uuidPattern.hasMatch(value)) {
    throw const AgentClientException(
      kind: AgentClientFailureKind.invalidRequest,
    );
  }
}

void _requireClientIdentity(String value, {int minimumLength = 1}) {
  if (value.runes.length < minimumLength ||
      value.runes.length > 128 ||
      !_clientIdentityPattern.hasMatch(value)) {
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

AgentClientException _invalidResponse() {
  return const AgentClientException(
    kind: AgentClientFailureKind.invalidResponse,
    retryable: true,
  );
}

String? _normalizedErrorCode(int status, String? decoded) {
  final allowed = switch ((status, decoded)) {
    (HttpStatus.badRequest, 'invalid_request') => decoded,
    (HttpStatus.unauthorized, 'authentication_required') => decoded,
    (HttpStatus.notFound, 'resource_not_found') => decoded,
    (HttpStatus.conflict, 'idempotency_key_conflict') => decoded,
    (HttpStatus.conflict, 'resource_conflict') => decoded,
    (HttpStatus.tooManyRequests, 'rate_limited') => decoded,
    (>= 500, 'internal_error') => decoded,
    _ => null,
  };
  return allowed ??
      switch (status) {
        HttpStatus.badRequest => 'invalid_request',
        HttpStatus.unauthorized => 'authentication_required',
        HttpStatus.notFound => 'resource_not_found',
        HttpStatus.conflict => 'resource_conflict',
        HttpStatus.tooManyRequests => 'rate_limited',
        >= 500 => 'internal_error',
        _ => null,
      };
}

String? _header(Map<String, String> headers, String name) {
  final target = name.toLowerCase();
  for (final entry in headers.entries) {
    if (entry.key.toLowerCase() == target) {
      return entry.value;
    }
  }
  return null;
}

bool _isWave(List<int> bytes) {
  return bytes.length >= 12 &&
      bytes[0] == 0x52 &&
      bytes[1] == 0x49 &&
      bytes[2] == 0x46 &&
      bytes[3] == 0x46 &&
      bytes[8] == 0x57 &&
      bytes[9] == 0x41 &&
      bytes[10] == 0x56 &&
      bytes[11] == 0x45;
}

void _zero(Uint8List bytes) {
  if (bytes.isNotEmpty) {
    bytes.fillRange(0, bytes.length, 0);
  }
}

DateTime _utcNow() => DateTime.now().toUtc();

final RegExp _uuidPattern = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);
final RegExp _clientIdentityPattern = RegExp(
  r'^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$',
);
final RegExp _providerPattern = RegExp(r'^[a-z][a-z0-9_-]{0,63}$');
final RegExp _failurePattern = RegExp(r'^[a-z][a-z0-9_]{0,63}$');
final RegExp _playbackPathPattern = RegExp(
  r'^/v1/agent-message-audios/[0-9a-f-]+/playback$',
);

final class _InvalidVoiceResponse implements Exception {
  const _InvalidVoiceResponse();
}
