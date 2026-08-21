import 'dart:convert';

import 'package:speakup/features/agent/client_action/agent_client_action.dart';

const _clientActionFields = <String>{'type', 'payload'};
const _maximumClientActions = 4;
const _maximumClientActionPayloadBytes = 16 * 1024;

final _clientActionTypePattern = RegExp(r'^[A-Za-z][A-Za-z0-9._:-]{0,63}$');

List<AgentClientAction> decodeAgentClientActions(Object? value) {
  if (value is! List || value.length > _maximumClientActions) {
    _rejectClientActionPayload();
  }
  return List<AgentClientAction>.unmodifiable(value.map(_decodeClientAction));
}

AgentClientAction _decodeClientAction(Object? value) {
  if (value is! Map) {
    _rejectClientActionPayload();
  }
  final object = <String, Object?>{};
  for (final entry in value.entries) {
    final key = entry.key;
    if (key is! String ||
        !_clientActionFields.contains(key) ||
        object.containsKey(key)) {
      _rejectClientActionPayload();
    }
    object[key] = entry.value;
  }
  if (object.length != _clientActionFields.length) {
    _rejectClientActionPayload();
  }
  final type = object['type'];
  final rawPayload = object['payload'];
  if (type is! String ||
      !_clientActionTypePattern.hasMatch(type) ||
      rawPayload is! Map) {
    _rejectClientActionPayload();
  }
  final payload = _copyJsonObject(rawPayload);
  if (utf8.encode(jsonEncode(payload)).length >
      _maximumClientActionPayloadBytes) {
    _rejectClientActionPayload();
  }
  return AgentClientAction(
    type: type,
    payload: Map<String, Object?>.unmodifiable(payload),
  );
}

Map<String, Object?> _copyJsonObject(Map<Object?, Object?> value) {
  final result = <String, Object?>{};
  for (final entry in value.entries) {
    final key = entry.key;
    if (key is! String || result.containsKey(key)) {
      _rejectClientActionPayload();
    }
    result[key] = _copyJsonValue(entry.value);
  }
  return result;
}

Object? _copyJsonValue(Object? value) => switch (value) {
  null || bool() || String() || num() => value,
  List() => List<Object?>.unmodifiable(value.map(_copyJsonValue)),
  Map() => Map<String, Object?>.unmodifiable(_copyJsonObject(value)),
  _ => _rejectClientActionPayload(),
};

Never _rejectClientActionPayload() {
  throw const FormatException('Invalid Agent client action payload.');
}
