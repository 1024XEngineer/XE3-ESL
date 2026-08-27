import 'dart:convert';

import 'package:speakup/features/agent/client_action/agent_client_action.dart';
import 'package:speakup/features/agent/client_action/agent_client_action_codec.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';

/// A protocol-shape failure. Transport adapters map this to their public
/// invalid-response error without duplicating the wire contract.
final class AgentWireCodecException implements Exception {
  const AgentWireCodecException();
}

final class AgentWireMessage {
  const AgentWireMessage({
    required this.id,
    required this.threadId,
    required this.role,
    required this.content,
    required this.sequence,
    required this.createdAt,
    required this.modality,
    this.clientMessageId,
    this.producedByRunId,
    this.audio,
    this.images = const <AgentImageAsset>[],
    this.clientActions = const <AgentClientAction>[],
    this.speechFeedbackStatusUrl,
  });

  final String id;
  final String threadId;
  final AgentMessageRole role;
  final String content;
  final int sequence;
  final DateTime createdAt;
  final AgentMessageModality modality;
  final String? clientMessageId;
  final String? producedByRunId;
  final AgentMessageAudio? audio;
  final List<AgentImageAsset> images;
  final List<AgentClientAction> clientActions;
  final String? speechFeedbackStatusUrl;

  AgentMessage get presentation => AgentMessage(
    id: id,
    role: role,
    text: content,
    sequence: sequence,
    createdAt: createdAt,
    clientMessageId: clientMessageId,
    producedByRunId: producedByRunId,
    modality: modality,
    audio: audio,
    images: images,
    clientActions: clientActions,
    speechFeedbackStatusUrl: speechFeedbackStatusUrl,
  );
}

final class AgentWireMessagePage {
  const AgentWireMessagePage({required this.messages, this.nextCursor});

  final List<AgentWireMessage> messages;
  final String? nextCursor;

  AgentMessagePage get presentation => AgentMessagePage(
    messages: List<AgentMessage>.unmodifiable(
      messages.map((message) => message.presentation),
    ),
    nextCursor: nextCursor,
  );
}

AgentWireMessagePage decodeAgentWireMessagePage(
  Object? value, {
  required String expectedThreadId,
}) {
  final root = _strictObject(
    value,
    allowed: const <String>{'messages', 'next_cursor'},
    required: const <String>{'messages'},
  );
  final values = _strictList(root['messages'], maxLength: 100);
  final result = <AgentWireMessage>[];
  final messageIds = <String>{};
  var previousSequence = 0;
  for (final value in values) {
    final message = decodeAgentWireMessage(
      value,
      expectedThreadId: expectedThreadId,
    );
    if (!messageIds.add(message.id) || message.sequence <= previousSequence) {
      throw const AgentWireCodecException();
    }
    previousSequence = message.sequence;
    result.add(message);
  }
  return AgentWireMessagePage(
    messages: List<AgentWireMessage>.unmodifiable(result),
    nextCursor: _absentOnlyOptional(
      root,
      'next_cursor',
      (value) => _strictString(value, minLength: 1, maxLength: 1024),
    ),
  );
}

AgentWireMessage decodeAgentWireMessage(
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
      'images',
      'client_actions',
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
  final id = _strictUuid(object['message_id']);
  final threadId = _strictUuid(object['thread_id']);
  if (threadId != expectedThreadId) {
    throw const AgentWireCodecException();
  }
  final sequence = _strictInt(object['sequence'], minimum: 1);
  final role = switch (_strictString(
    object['role'],
    minLength: 1,
    maxLength: 16,
  )) {
    'user' => AgentMessageRole.user,
    'assistant' => AgentMessageRole.assistant,
    _ => throw const AgentWireCodecException(),
  };
  final content = _strictContent(object['content']);
  final createdAt = _strictDateTime(object['created_at']);
  final modality =
      _absentOnlyOptional(
        object,
        'modality',
        (value) => switch (_strictString(value, minLength: 1, maxLength: 16)) {
          'voice' => AgentMessageModality.voice,
          'multimodal' => AgentMessageModality.multimodal,
          _ => throw const AgentWireCodecException(),
        },
      ) ??
      AgentMessageModality.text;
  final audio = _absentOnlyOptional(
    object,
    'audio',
    decodeAgentWireMessageAudio,
  );
  final images =
      _absentOnlyOptional(object, 'images', decodeAgentWireMessageImages) ??
      const <AgentImageAsset>[];
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
  final clientActions =
      _absentOnlyOptional(object, 'client_actions', decodeAgentClientActions) ??
      const <AgentClientAction>[];
  final speechFeedbackStatusUrl = _absentOnlyOptional(
    object,
    'speech_feedback_status_url',
    (value) {
      final path = _strictString(value, minLength: 1, maxLength: 160);
      if (!validAgentSpeechFeedbackStatusUrl(path)) {
        throw const AgentWireCodecException();
      }
      return path;
    },
  );

  if ((role == AgentMessageRole.user &&
          (clientMessageId == null ||
              producedByRunId != null ||
              clientActions.isNotEmpty)) ||
      (role == AgentMessageRole.assistant &&
          (clientMessageId != null || producedByRunId == null)) ||
      (modality != AgentMessageModality.voice && audio != null) ||
      (modality == AgentMessageModality.multimodal &&
          (role != AgentMessageRole.user || audio != null || images.isEmpty)) ||
      (modality != AgentMessageModality.multimodal && images.isNotEmpty) ||
      (modality == AgentMessageModality.voice &&
          role != AgentMessageRole.user) ||
      (speechFeedbackStatusUrl != null &&
          (role != AgentMessageRole.user ||
              modality != AgentMessageModality.voice ||
              audio == null))) {
    throw const AgentWireCodecException();
  }

  return AgentWireMessage(
    id: id,
    threadId: threadId,
    role: role,
    content: content,
    sequence: sequence,
    createdAt: createdAt,
    modality: modality,
    clientMessageId: clientMessageId,
    producedByRunId: producedByRunId,
    audio: audio,
    images: images,
    clientActions: clientActions,
    speechFeedbackStatusUrl: speechFeedbackStatusUrl,
  );
}

List<AgentImageAsset> decodeAgentWireMessageImages(Object? value) {
  final values = _strictList(value, maxLength: agentMaximumImagesPerMessage);
  if (values.isEmpty) {
    throw const AgentWireCodecException();
  }
  final ids = <String>{};
  return List<AgentImageAsset>.unmodifiable(
    values.map((item) {
      final object = _strictObject(
        item,
        allowed: const <String>{
          'image_asset_id',
          'content_type',
          'size_bytes',
          'width',
          'height',
          'status',
          'created_at',
          'attached_at',
        },
        required: const <String>{
          'image_asset_id',
          'content_type',
          'size_bytes',
          'width',
          'height',
          'status',
          'created_at',
        },
      );
      final id = _strictUuid(object['image_asset_id']);
      final contentType = _strictString(
        object['content_type'],
        minLength: 1,
        maxLength: 32,
      );
      final status = switch (_strictString(
        object['status'],
        minLength: 1,
        maxLength: 16,
      )) {
        'staged' => AgentImageAssetStatus.staged,
        'ready' => AgentImageAssetStatus.ready,
        'deleting' => AgentImageAssetStatus.deleting,
        _ => throw const AgentWireCodecException(),
      };
      if (!ids.add(id) || !_supportedImageContentTypes.contains(contentType)) {
        throw const AgentWireCodecException();
      }
      return AgentImageAsset(
        id: id,
        contentType: contentType,
        sizeBytes: _strictInt(
          object['size_bytes'],
          minimum: 1,
          maximum: agentMaximumImageBytes,
        ),
        width: _strictInt(object['width'], minimum: 1, maximum: 16384),
        height: _strictInt(object['height'], minimum: 1, maximum: 16384),
        status: status,
        createdAt: _strictDateTime(object['created_at']),
        attachedAt: _absentOnlyOptional(object, 'attached_at', _strictDateTime),
      );
    }),
  );
}

AgentMessageAudio decodeAgentWireMessageAudio(Object? value) {
  final object = _strictObject(
    value,
    allowed: const <String>{
      'audio_id',
      'status',
      'content_type',
      'size_bytes',
      'duration_ms',
      'playback_path',
    },
    required: const <String>{
      'audio_id',
      'status',
      'content_type',
      'size_bytes',
      'duration_ms',
      'playback_path',
    },
  );
  final id = _strictUuid(object['audio_id']);
  if (_strictString(object['status'], minLength: 1, maxLength: 16) !=
          'readable' ||
      _strictString(object['content_type'], minLength: 1, maxLength: 32) !=
          'audio/wav') {
    throw const AgentWireCodecException();
  }
  final playbackPath = _strictPatternString(
    object['playback_path'],
    pattern: _agentMessageAudioPlaybackPathPattern,
    maxLength: 256,
  );
  if (playbackPath != '/v1/agent-message-audios/$id/playback') {
    throw const AgentWireCodecException();
  }
  return AgentMessageAudio(
    id: id,
    status: AgentMessageAudioStatus.readable,
    contentType: 'audio/wav',
    sizeBytes: _strictInt(object['size_bytes'], minimum: 1, maximum: 7400000),
    duration: Duration(
      milliseconds: _strictInt(
        object['duration_ms'],
        minimum: 1,
        maximum: 60000,
      ),
    ),
    playbackPath: playbackPath,
  );
}

AgentRun decodeAgentWireRun(Object? value) {
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
      'completion_source',
      'provider_completion_id',
      'provider_model',
      'finish_reason',
      'usage',
      'domain_tool_call_id',
      'domain_tool_name',
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
  final attempt = _strictInt(object['attempt'], minimum: 1);
  final retryOfRunId = _nullableOptional(
    object,
    'retry_of_run_id',
    _strictUuid,
  );
  final clientRetryId = _nullableOptional(
    object,
    'client_retry_id',
    _strictClientIdentity,
  );
  final hasRetryIdentity = retryOfRunId != null && clientRetryId != null;
  if ((retryOfRunId == null) != (clientRetryId == null) ||
      (attempt == 1 ? hasRetryIdentity : !hasRetryIdentity)) {
    throw const AgentWireCodecException();
  }
  final status = switch (_strictString(
    object['status'],
    minLength: 1,
    maxLength: 16,
  )) {
    'pending' => AgentRunStatus.pending,
    'running' => AgentRunStatus.running,
    'completed' => AgentRunStatus.completed,
    'failed' => AgentRunStatus.failed,
    _ => throw const AgentWireCodecException(),
  };
  final requestedProvider = _strictPatternString(
    object['requested_provider'],
    pattern: _providerPattern,
    maxLength: 64,
  );
  final requestedModel = _strictModelIdentity(object['requested_model']);
  final maxOutputTokens = _strictInt(object['max_output_tokens'], minimum: 1);
  final createdAt = _strictDateTime(object['created_at']);
  final updatedAt = _strictDateTime(object['updated_at']);
  if (updatedAt.isBefore(createdAt)) {
    throw const AgentWireCodecException();
  }
  final startedAt = _nullableOptional(object, 'started_at', _strictDateTime);
  final completedAt = _nullableOptional(
    object,
    'completed_at',
    _strictDateTime,
  );
  if ((startedAt != null && startedAt.isBefore(createdAt)) ||
      (completedAt != null && startedAt == null) ||
      (completedAt != null && completedAt.isBefore(startedAt!))) {
    throw const AgentWireCodecException();
  }

  final assistantMessageId = _nullableOptional(
    object,
    'assistant_message_id',
    _strictUuid,
  );
  final failureObject = object['failure'] == null
      ? null
      : _strictObject(
          object['failure'],
          allowed: const <String>{'kind', 'retryable'},
          required: const <String>{'kind', 'retryable'},
        );
  final failure = failureObject == null
      ? null
      : AgentRunFailure(
          kind: _decodeRunFailureKind(failureObject['kind']),
          retryable: _strictBool(failureObject['retryable']),
        );

  AgentRunCompletion? completion;
  switch (status) {
    case AgentRunStatus.completed:
      if (assistantMessageId == null ||
          failure != null ||
          startedAt == null ||
          completedAt == null) {
        throw const AgentWireCodecException();
      }
      completion = _decodeRunCompletion(object);
    case AgentRunStatus.failed:
      if (failure == null ||
          assistantMessageId != null ||
          startedAt == null ||
          completedAt == null ||
          _hasCompletionFields(object)) {
        throw const AgentWireCodecException();
      }
    case AgentRunStatus.pending:
      if (assistantMessageId != null ||
          failure != null ||
          startedAt != null ||
          completedAt != null ||
          _hasCompletionFields(object)) {
        throw const AgentWireCodecException();
      }
    case AgentRunStatus.running:
      if (assistantMessageId != null ||
          failure != null ||
          startedAt == null ||
          completedAt != null ||
          _hasCompletionFields(object)) {
        throw const AgentWireCodecException();
      }
  }

  return AgentRun(
    id: id,
    threadId: threadId,
    inputMessageId: inputMessageId,
    attempt: attempt,
    retryOfRunId: retryOfRunId,
    clientRetryId: clientRetryId,
    status: status,
    requestedProvider: requestedProvider,
    requestedModel: requestedModel,
    maxOutputTokens: maxOutputTokens,
    assistantMessageId: assistantMessageId,
    completion: completion,
    failure: failure,
    createdAt: createdAt,
    startedAt: startedAt,
    completedAt: completedAt,
    updatedAt: updatedAt,
  );
}

/// Whether two representations refer to the same immutable durable Run.
///
/// Status, terminal result, and update timestamps may advance while these
/// creation-time fields must remain frozen across POST, SSE, and recovery GET.
bool sameAgentRunIdentity(AgentRun current, AgentRun initial) {
  return current.id == initial.id &&
      current.threadId == initial.threadId &&
      current.inputMessageId == initial.inputMessageId &&
      current.attempt == initial.attempt &&
      current.retryOfRunId == initial.retryOfRunId &&
      current.clientRetryId == initial.clientRetryId &&
      current.requestedProvider == initial.requestedProvider &&
      current.requestedModel == initial.requestedModel &&
      current.maxOutputTokens == initial.maxOutputTokens &&
      current.createdAt == initial.createdAt;
}

AgentRunCompletion _decodeRunCompletion(Map<String, Object?> object) {
  final source = _strictString(
    object['completion_source'],
    minLength: 1,
    maxLength: 16,
  );
  switch (source) {
    case 'model':
      if (object.containsKey('domain_tool_call_id') ||
          object.containsKey('domain_tool_name')) {
        throw const AgentWireCodecException();
      }
      final finishReason = _strictString(
        object['finish_reason'],
        minLength: 1,
        maxLength: 16,
      );
      if (finishReason != 'stop' && finishReason != 'length') {
        throw const AgentWireCodecException();
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
      return AgentModelRunCompletion(
        providerCompletionId: _strictClientIdentity(
          object['provider_completion_id'],
        ),
        providerModel: _strictModelIdentity(object['provider_model']),
        finishReason: finishReason,
        usage: AgentRunUsage(
          inputTokens: _strictInt(usage['input_tokens'], minimum: 0),
          outputTokens: _strictInt(usage['output_tokens'], minimum: 0),
          totalTokens: _strictInt(usage['total_tokens'], minimum: 0),
        ),
      );
    case 'domain':
      if (object.containsKey('provider_completion_id') ||
          object.containsKey('provider_model') ||
          object.containsKey('finish_reason') ||
          object.containsKey('usage')) {
        throw const AgentWireCodecException();
      }
      return AgentDomainRunCompletion(
        toolCallId: _strictClientIdentity(object['domain_tool_call_id']),
        toolName: _strictClientIdentity(object['domain_tool_name']),
      );
    default:
      throw const AgentWireCodecException();
  }
}

bool _hasCompletionFields(Map<String, Object?> object) {
  return _completionFields.any(object.containsKey);
}

String _decodeRunFailureKind(Object? value) {
  return _strictPatternString(
    value,
    pattern: _failureKindPattern,
    maxLength: 64,
  );
}

Map<String, Object?> _strictObject(
  Object? value, {
  required Set<String> allowed,
  required Set<String> required,
}) {
  if (value is! Map) {
    throw const AgentWireCodecException();
  }
  final result = <String, Object?>{};
  for (final entry in value.entries) {
    if (entry.key is! String ||
        !allowed.contains(entry.key) ||
        result.containsKey(entry.key)) {
      throw const AgentWireCodecException();
    }
    result[entry.key as String] = entry.value;
  }
  if (!result.keys.toSet().containsAll(required)) {
    throw const AgentWireCodecException();
  }
  return result;
}

List<Object?> _strictList(Object? value, {required int maxLength}) {
  if (value is! List || value.length > maxLength) {
    throw const AgentWireCodecException();
  }
  return List<Object?>.of(value);
}

String _strictString(
  Object? value, {
  required int minLength,
  required int maxLength,
}) {
  if (value is! String ||
      value.runes.length < minLength ||
      value.runes.length > maxLength) {
    throw const AgentWireCodecException();
  }
  return value;
}

String _strictContent(Object? value) {
  final content = _strictString(value, minLength: 1, maxLength: 4096);
  if (content.trim().isEmpty || utf8.encode(content).length > 16384) {
    throw const AgentWireCodecException();
  }
  return content;
}

String _strictPatternString(
  Object? value, {
  required RegExp pattern,
  required int maxLength,
}) {
  final result = _strictString(value, minLength: 1, maxLength: maxLength);
  if (!pattern.hasMatch(result)) {
    throw const AgentWireCodecException();
  }
  return result;
}

String _strictUuid(Object? value) =>
    _strictPatternString(value, pattern: _uuidPattern, maxLength: 36);

String _strictClientIdentity(Object? value) => _strictPatternString(
  value,
  pattern: _clientIdentityPattern,
  maxLength: 128,
);

String _strictModelIdentity(Object? value) =>
    _strictPatternString(value, pattern: _modelIdentityPattern, maxLength: 128);

int _strictInt(Object? value, {required int minimum, int? maximum}) {
  if (value is! int ||
      value < minimum ||
      (maximum != null && value > maximum)) {
    throw const AgentWireCodecException();
  }
  return value;
}

bool _strictBool(Object? value) {
  if (value is! bool) {
    throw const AgentWireCodecException();
  }
  return value;
}

DateTime _strictDateTime(Object? value) {
  final raw = _strictString(value, minLength: 1, maxLength: 64);
  final parsed = DateTime.tryParse(raw);
  if (parsed == null || !raw.contains(RegExp(r'(Z|[+-]\d{2}:\d{2})$'))) {
    throw const AgentWireCodecException();
  }
  return parsed.toUtc();
}

T? _absentOnlyOptional<T>(
  Map<String, Object?> object,
  String key,
  T Function(Object? value) decode,
) {
  if (!object.containsKey(key)) {
    return null;
  }
  return decode(object[key]);
}

T? _nullableOptional<T>(
  Map<String, Object?> object,
  String key,
  T Function(Object? value) decode,
) {
  final value = object[key];
  return value == null ? null : decode(value);
}

const Set<String> _supportedImageContentTypes = <String>{
  'image/jpeg',
  'image/png',
  'image/webp',
};
const Set<String> _completionFields = <String>{
  'completion_source',
  'provider_completion_id',
  'provider_model',
  'finish_reason',
  'usage',
  'domain_tool_call_id',
  'domain_tool_name',
};
final RegExp _uuidPattern = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);
final RegExp _clientIdentityPattern = RegExp(
  r'^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$',
);
final RegExp _modelIdentityPattern = RegExp(
  r'^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._:-]*)*$',
);
final RegExp _providerPattern = RegExp(r'^[a-z][a-z0-9_-]{0,63}$');
final RegExp _failureKindPattern = RegExp(r'^[a-z][a-z0-9_]{0,63}$');
final RegExp _agentMessageAudioPlaybackPathPattern = RegExp(
  r'^/v1/agent-message-audios/[0-9a-f-]+/playback$',
);
