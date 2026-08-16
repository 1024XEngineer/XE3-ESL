import 'dart:convert';

const evaluationId = '10000000-0000-4000-8000-000000000001';
const practiceTurnId = '20000000-0000-4000-8000-000000000001';
const practiceSessionId = '30000000-0000-4000-8000-000000000001';
const feedbackItemId = '40000000-0000-4000-8000-000000000001';
const agentMessageId = '50000000-0000-4000-8000-000000000001';
const agentThreadId = '60000000-0000-4000-8000-000000000001';

const practiceStatusUrl = '/v1/practice-turns/$practiceTurnId/evaluation';
const agentStatusUrl = '/v1/agent-messages/$agentMessageId/evaluation';

Map<String, Object?> readyPracticeFeedbackFixture() => <String, Object?>{
  'evaluation_id': evaluationId,
  'kind': 'PRACTICE_TURN_FEEDBACK',
  'source_id': practiceTurnId,
  'context_id': practiceSessionId,
  'status': 'READY',
  'created_at': '2026-08-15T01:00:00.000Z',
  'updated_at': '2026-08-15T01:00:01.000Z',
  'feedback_items': <Object?>[
    <String, Object?>{
      'feedback_item_id': feedbackItemId,
      'evaluation_id': evaluationId,
      'position': 1,
      'category': 'CORRECTION',
      'severity': 'MEDIUM',
      'evidence': <String, Object?>{
        'evidence_ref_id': practiceTurnId,
        'start_utf8_byte': 0,
        'end_utf8_byte': 8,
        'original_excerpt': 'I manage',
      },
      'recommendation': 'Use the past tense.',
      'correction': 'I managed the project.',
      'repractice_mode': 'SAME_QUESTION',
      'created_at': '2026-08-15T01:00:01.000Z',
    },
  ],
  'result': <String, Object?>{
    'schema_version': 'speech-feedback/v1',
    'scoreability_status': 'PROVISIONAL',
    'summary': 'Good structure; correct the tense.',
    'reason_codes': <Object?>[],
    'acoustic': <String, Object?>{
      'status': 'NOT_ASSESSED',
      'reason': 'ACOUSTIC_ASSESSMENT_NOT_CONFIGURED',
    },
  },
};

Map<String, Object?> readyAgentFeedbackFixture() {
  final value = readyPracticeFeedbackFixture()
    ..['kind'] = 'AGENT_MESSAGE_FEEDBACK'
    ..['source_id'] = agentMessageId
    ..['context_id'] = agentThreadId;
  final item =
      (value['feedback_items']! as List<Object?>).single
          as Map<String, Object?>;
  item['repractice_mode'] = 'NONE';
  (item['evidence']! as Map<String, Object?>)['evidence_ref_id'] =
      agentMessageId;
  return value;
}

Map<String, Object?> pendingPracticeFeedbackFixture(String status) =>
    <String, Object?>{
      'evaluation_id': evaluationId,
      'kind': 'PRACTICE_TURN_FEEDBACK',
      'source_id': practiceTurnId,
      'context_id': practiceSessionId,
      'status': status,
      'created_at': '2026-08-15T01:00:00.000Z',
      'updated_at': '2026-08-15T01:00:00.000Z',
      'feedback_items': <Object?>[],
    };

Map<String, Object?> failedPracticeFeedbackFixture() => <String, Object?>{
  ...pendingPracticeFeedbackFixture('FAILED'),
  'updated_at': '2026-08-15T01:00:02.000Z',
  'error': <String, Object?>{
    'code': 'PROVIDER_UNAVAILABLE',
    'retryable': true,
    'message': 'Feedback provider is temporarily unavailable.',
  },
};

Map<String, Object?> cloneFeedbackFixture(Object? value) =>
    jsonDecode(jsonEncode(value)) as Map<String, Object?>;
