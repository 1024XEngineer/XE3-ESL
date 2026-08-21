import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation.dart';

import 'evaluation_report_fixture.dart';

void main() {
  test('decodes a ready session evaluation from the canonical resource', () {
    final stored = evaluationReportWireFixture();
    final evaluation = decodeSessionEvaluation(<String, Object?>{
      'evaluation_id': stored['evaluation_id'],
      'kind': 'SESSION_REPORT',
      'source_id': stored['practice_session_id'],
      'context_id': stored['practice_session_id'],
      'status': 'READY',
      'created_at': stored['created_at'],
      'updated_at': stored['created_at'],
      'feedback_items': <Object?>[],
      'result': stored['report'],
    });

    expect(evaluation.status, SessionEvaluationStatus.ready);
    expect(evaluation.report?.summary, '本次练习已形成面试表达评估。');
  });

  test('decodes a failed session evaluation', () {
    final evaluation = decodeSessionEvaluation(<String, Object?>{
      'evaluation_id': '70000000-0000-4000-8000-000000000007',
      'kind': 'SESSION_REPORT',
      'source_id': '30000000-0000-4000-8000-000000000003',
      'context_id': '30000000-0000-4000-8000-000000000003',
      'status': 'FAILED',
      'created_at': '2026-08-16T10:00:00Z',
      'updated_at': '2026-08-16T10:01:00Z',
      'feedback_items': <Object?>[],
      'error': <String, Object?>{
        'code': 'PROVIDER_UNAVAILABLE',
        'retryable': true,
        'message': '评分服务暂不可用。',
      },
    });

    expect(evaluation.status, SessionEvaluationStatus.failed);
    expect(evaluation.failure?.retryable, isTrue);
  });

  test('rejects feedback items and result in a pending resource', () {
    final base = <String, Object?>{
      'evaluation_id': '70000000-0000-4000-8000-000000000007',
      'kind': 'SESSION_REPORT',
      'source_id': '30000000-0000-4000-8000-000000000003',
      'context_id': '30000000-0000-4000-8000-000000000003',
      'status': 'QUEUED',
      'created_at': '2026-08-16T10:00:00Z',
      'updated_at': '2026-08-16T10:00:00Z',
      'feedback_items': <Object?>[],
    };
    final withItem = <String, Object?>{
      ...base,
      'feedback_items': <Object?>[<String, Object?>{}],
    };
    final withResult = <String, Object?>{
      ...base,
      'result': <String, Object?>{},
    };

    expect(
      () => decodeSessionEvaluation(withItem),
      throwsA(isA<SessionEvaluationDecodeException>()),
    );
    expect(
      () => decodeSessionEvaluation(withResult),
      throwsA(isA<SessionEvaluationDecodeException>()),
    );
  });
}
