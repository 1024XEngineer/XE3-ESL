import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/providers/agent/agent_wire_codec.dart';

void main() {
  test('accepts every current server Agent Run failure kind', () {
    for (final kind in const <String>{
      'interrupted',
      'configuration_drift',
      'invalid_context',
      'tool_iteration_budget_exhausted',
      'tool_call_budget_exhausted',
      'duplicate_tool_call',
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
    }) {
      final run = decodeAgentWireRun(_failedRun(kind));

      expect(run.status, AgentRunStatus.failed, reason: kind);
      expect(run.failure?.kind, kind, reason: kind);
    }
  });

  test('keeps the OpenAPI failure kind extensible but syntax-strict', () {
    expect(
      decodeAgentWireRun(_failedRun('future_provider_failure')).failure?.kind,
      'future_provider_failure',
    );
    for (final kind in <String>[
      'UPPERCASE',
      'contains-hyphen',
      '1_starts_with_a_number',
      List<String>.filled(65, 'a').join(),
    ]) {
      expect(
        () => decodeAgentWireRun(_failedRun(kind)),
        throwsA(isA<AgentWireCodecException>()),
        reason: kind,
      );
    }
  });

  test('requires initial and retry attempt identities to agree', () {
    final initialWithRetryIdentity = _failedRun('internal_error')
      ..addAll(<String, Object?>{
        'retry_of_run_id': '10000000-0000-4000-8000-000000000000',
        'client_retry_id': 'retry:10000000-0000-4000-8000-000000000000',
      });
    final retryWithoutRetryIdentity = _failedRun('internal_error')
      ..['attempt'] = 2;

    expect(
      () => decodeAgentWireRun(initialWithRetryIdentity),
      throwsA(isA<AgentWireCodecException>()),
    );
    expect(
      () => decodeAgentWireRun(retryWithoutRetryIdentity),
      throwsA(isA<AgentWireCodecException>()),
    );
  });

  test('accepts provider-qualified model identities only', () {
    final qualified = _failedRun('internal_error')
      ..['requested_model'] = 'qwen/qwen3.7-plus';
    expect(decodeAgentWireRun(qualified).requestedModel, 'qwen/qwen3.7-plus');

    for (final model in <String>[
      'qwen//qwen3.7-plus',
      'qwen/../qwen3.7-plus',
      'qwen/model..variant',
      '/qwen3.7-plus',
    ]) {
      final invalid = _failedRun('internal_error')..['requested_model'] = model;
      expect(
        () => decodeAgentWireRun(invalid),
        throwsA(isA<AgentWireCodecException>()),
        reason: model,
      );
    }
  });
}

Map<String, Object?> _failedRun(String kind) => <String, Object?>{
  'run_id': '10000000-0000-4000-8000-000000000001',
  'thread_id': '20000000-0000-4000-8000-000000000001',
  'input_message_id': '30000000-0000-4000-8000-000000000001',
  'attempt': 1,
  'status': 'failed',
  'requested_provider': 'qianwen',
  'requested_model': 'qwen-plus',
  'max_output_tokens': 1024,
  'failure': <String, Object?>{'kind': kind, 'retryable': false},
  'created_at': '2026-08-18T00:00:00Z',
  'started_at': '2026-08-18T00:00:01Z',
  'completed_at': '2026-08-18T00:00:02Z',
  'updated_at': '2026-08-18T00:00:02Z',
};
