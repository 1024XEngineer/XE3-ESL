import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/review_summary.dart';

EvaluationReport evaluationReportFixture({
  required ReviewSummary review,
  required String practiceSessionId,
  required DateTime completedAt,
  double score = 80,
  EvaluationReportSceneType sceneType = EvaluationReportSceneType.interview,
  EvaluationReportScoreability scoreability =
      EvaluationReportScoreability.provisional,
}) {
  final finding = EvaluationReportFinding(
    id: 'improvement_action',
    message: review.nextFocus,
    suggestion: review.nextFocus,
    evidence: const <EvaluationReportEvidence>[],
  );
  return EvaluationReport(
    id: review.id,
    evaluationId: '7b000001-0000-4000-8000-000000000001',
    practiceSessionId: practiceSessionId,
    sceneType: sceneType,
    practiceExperience: switch (sceneType) {
      EvaluationReportSceneType.ieltsSpeaking => 'IELTS_SPEAKING',
      EvaluationReportSceneType.interview => 'INTERVIEW',
      EvaluationReportSceneType.overseasDailyLife => 'LIFE_AND_TRAVEL',
      EvaluationReportSceneType.overseasWorkplace => 'WORKPLACE',
    },
    sceneCategory: switch (sceneType) {
      EvaluationReportSceneType.ieltsSpeaking => 'IELTS_SPEAKING',
      EvaluationReportSceneType.interview => 'INTERVIEW_PROFESSIONAL',
      EvaluationReportSceneType.overseasDailyLife => 'LIFE_TRAVEL',
      EvaluationReportSceneType.overseasWorkplace => 'WORKPLACE_GENERAL',
    },
    practiceMode: sceneType == EvaluationReportSceneType.ieltsSpeaking
        ? 'FULL_MOCK'
        : 'FULL_SIMULATION',
    scoreability: scoreability,
    summary: review.summary,
    questions: const <EvaluationReportQuestion>[
      EvaluationReportQuestion(
        id: '40000000-0000-4000-8000-000000000001',
        position: 1,
        text: 'Tell me about your experience.',
        answer: EvaluationReportAnswer(
          turnId: '50000000-0000-4000-8000-000000000005',
          transcript: 'I led a project and improved delivery.',
        ),
      ),
    ],
    dimensions: <EvaluationReportDimension>[
      EvaluationReportDimension(
        key: 'INTERVIEW_STRUCTURE',
        score: scoreability == EvaluationReportScoreability.insufficient
            ? null
            : score,
        scale: EvaluationReportScoreScale.percentage100,
        coverage: scoreability == EvaluationReportScoreability.insufficient
            ? 0
            : 1,
        confidence: scoreability == EvaluationReportScoreability.insufficient
            ? 0
            : 0.8,
        reasonCodes: <String>[
          scoreability == EvaluationReportScoreability.insufficient
              ? 'INSUFFICIENT_EVIDENCE'
              : 'ASR_CONFIDENCE_UNAVAILABLE',
        ],
        evidenceRefIds:
            scoreability == EvaluationReportScoreability.insufficient
            ? const <String>[]
            : const <String>['evidence_demo_001'],
        strengths: scoreability == EvaluationReportScoreability.insufficient
            ? const <EvaluationReportFinding>[]
            : <EvaluationReportFinding>[
                EvaluationReportFinding(
                  id: 'strength_action',
                  message: review.strength,
                  evidence: const <EvaluationReportEvidence>[],
                ),
              ],
        improvements: scoreability == EvaluationReportScoreability.insufficient
            ? const <EvaluationReportFinding>[]
            : <EvaluationReportFinding>[finding],
        recommendedExamples: const <EvaluationReportFinding>[],
      ),
    ],
    priorityActions: scoreability == EvaluationReportScoreability.insufficient
        ? const <EvaluationReportPriorityAction>[]
        : const <EvaluationReportPriorityAction>[
            EvaluationReportPriorityAction(
              dimensionKey: 'INTERVIEW_STRUCTURE',
              findingId: 'improvement_action',
            ),
          ],
    createdAt: completedAt,
  );
}

Map<String, Object?> evaluationReportWireFixture({
  String reportId = '20000000-0000-4000-8000-000000000002',
  String practiceSessionId = '30000000-0000-4000-8000-000000000003',
  String createdAt = '2026-07-26T10:00:00Z',
  double score = 82,
  String sceneType = 'INTERVIEW',
  String practiceExperience = 'INTERVIEW',
  String sceneCategory = 'INTERVIEW_PROFESSIONAL',
  String practiceMode = 'FULL_SIMULATION',
  String scoreability = 'PROVISIONAL',
}) {
  final insufficient = scoreability == 'INSUFFICIENT';
  return <String, Object?>{
    'report_id': reportId,
    'evaluation_id': '7b000001-0000-4000-8000-000000000001',
    'practice_session_id': practiceSessionId,
    'report': <String, Object?>{
      'schema_version': 'evaluation-report/v2',
      'scene_type': sceneType,
      'practice_experience': practiceExperience,
      'scene_category': sceneCategory,
      'practice_mode': practiceMode,
      'scoreability_status': scoreability,
      'summary': insufficient ? '本次练习的有效证据不足，暂不形成能力结论。' : '本次练习已形成面试表达评估。',
      'questions': <Object?>[
        <String, Object?>{
          'question_id': '40000000-0000-4000-8000-000000000001',
          'position': 1,
          'text': 'Tell me about your experience.',
          'answer': <String, Object?>{
            'turn_id': '50000000-0000-4000-8000-000000000005',
            'transcript': 'I led a project and improved delivery.',
          },
        },
      ],
      'dimensions': <Object?>[
        <String, Object?>{
          'key': 'INTERVIEW_STRUCTURE',
          'score': insufficient ? null : score,
          'scale': 'PERCENTAGE_100',
          'coverage': insufficient ? 0 : 1,
          'confidence': insufficient ? 0 : 0.8,
          'reason_codes': <Object?>[
            insufficient
                ? 'INSUFFICIENT_EVIDENCE'
                : 'ASR_CONFIDENCE_UNAVAILABLE',
          ],
          'evidence_ref_ids': insufficient
              ? <Object?>[]
              : <Object?>['40000000-0000-4000-8000-000000000004'],
          'strengths': insufficient
              ? <Object?>[]
              : <Object?>[
                  <String, Object?>{
                    'finding_id': 'strength_action',
                    'message': '回答与问题相关。',
                    'evidence': <Object?>[],
                  },
                ],
          'improvements': insufficient
              ? <Object?>[]
              : <Object?>[
                  <String, Object?>{
                    'finding_id': 'improvement_action',
                    'message': '回答结构可以更清楚。',
                    'suggestion': '使用 STAR 结构。',
                    'evidence': <Object?>[
                      <String, Object?>{
                        'evidence_ref_id':
                            '40000000-0000-4000-8000-000000000004',
                        'turn_id': '50000000-0000-4000-8000-000000000005',
                        'start_utf8_byte': 0,
                        'end_utf8_byte': 26,
                        'original_excerpt': 'I made the product better.',
                      },
                    ],
                  },
                ],
          'recommended_examples': <Object?>[],
        },
      ],
      'priority_actions': insufficient
          ? <Object?>[]
          : <Object?>[
              <String, Object?>{
                'dimension_key': 'INTERVIEW_STRUCTURE',
                'finding_id': 'improvement_action',
              },
            ],
    },
    'created_at': createdAt,
  };
}
