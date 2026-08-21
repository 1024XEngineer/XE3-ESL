import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presenter.dart';

void main() {
  test('IELTS presents four axes and rounds overall score to half band', () {
    final report = _report(
      sceneType: EvaluationReportSceneType.ieltsSpeaking,
      practiceMode: 'PART_1',
      dimensions: <EvaluationReportDimension>[
        _dimension('FLUENCY_COHERENCE', 6.5),
        _dimension('LEXICAL_RESOURCE', 5.5),
        _dimension('GRAMMATICAL_RANGE_ACCURACY', 5.5),
        _dimension('PRONUNCIATION', 5),
      ],
    );

    final result = presentEvaluationReportDetail(report);

    expect(result.pageTitle, 'Part 1 专项复盘');
    expect(result.badgeLabel, 'IELTS Part 1');
    expect(result.radarAxes, hasLength(4));
    expect(result.overallScoreLabel, '5.5');
    expect(result.scaleSuffix, '/ 9');
  });

  test('IELTS missing pronunciation keeps an unavailable fourth axis', () {
    final report = _report(
      sceneType: EvaluationReportSceneType.ieltsSpeaking,
      dimensions: <EvaluationReportDimension>[
        _dimension('FLUENCY_COHERENCE', 6),
        _dimension('LEXICAL_RESOURCE', 6),
        _dimension('GRAMMATICAL_RANGE_ACCURACY', 6),
      ],
    );

    final result = presentEvaluationReportDetail(report);

    expect(result.radarAxes, hasLength(4));
    expect(result.radarAxes.last.label, '发音');
    expect(result.radarAxes.last.scoreLabel, '--');
    expect(result.radarAxes.last.normalizedValue, isNull);
    expect(result.dimensions.last.key, 'PRONUNCIATION');
    expect(result.dimensions.last.scoreLabel, '未评分');
    expect(result.overallScoreLabel, '--');
    expect(result.scaleSuffix, '/ 9');
  });

  test('interview presents five axes and an integer percentage average', () {
    final report = _report(
      sceneType: EvaluationReportSceneType.interview,
      dimensions: <EvaluationReportDimension>[
        _dimension('INTERVIEW_RELEVANCE', 80, percentage: true),
        _dimension('INTERVIEW_STRUCTURE', 70, percentage: true),
        _dimension('INTERVIEW_EVIDENCE', 60, percentage: true),
        _dimension('INTERVIEW_PROFESSIONAL', 90, percentage: true),
        _dimension('INTERVIEW_INTERACTION', 75, percentage: true),
      ],
    );

    final result = presentEvaluationReportDetail(report);

    expect(result.radarAxes, hasLength(5));
    expect(result.overallScoreLabel, '75');
    expect(result.scaleSuffix, '/ 100');
    expect(result.dimensions, hasLength(5));
  });

  test('priority dimension leads and priority improvement leads its card', () {
    const priority = EvaluationReportFinding(
      id: 'priority',
      message: '优先改进',
      suggestion: '先做这项',
      evidence: <EvaluationReportEvidence>[],
    );
    const secondary = EvaluationReportFinding(
      id: 'secondary',
      message: '普通改进',
      suggestion: '随后处理',
      evidence: <EvaluationReportEvidence>[],
    );
    final report = _report(
      sceneType: EvaluationReportSceneType.overseasWorkplace,
      dimensions: <EvaluationReportDimension>[
        _dimension('TASK_ACHIEVEMENT', 40, percentage: true),
        _dimension(
          'LANGUAGE_CONTROL',
          80,
          percentage: true,
          improvements: const <EvaluationReportFinding>[secondary, priority],
        ),
        _dimension('CLARITY_COHERENCE', 60, percentage: true),
        _dimension('INTERACTION', 70, percentage: true),
      ],
      priorityActions: const <EvaluationReportPriorityAction>[
        EvaluationReportPriorityAction(
          dimensionKey: 'LANGUAGE_CONTROL',
          findingId: 'priority',
        ),
      ],
    );

    final result = presentEvaluationReportDetail(report);

    expect(result.hasPriorityDimensions, isTrue);
    expect(result.dimensions.first.key, 'LANGUAGE_CONTROL');
    expect(result.dimensions.first.findings.first.id, 'priority');
    expect(result.dimensions.first.suggestions, <String>['先做这项', '随后处理']);
  });

  test('invalid priority action is ignored and low normalized score leads', () {
    final report = _report(
      sceneType: EvaluationReportSceneType.overseasDailyLife,
      dimensions: <EvaluationReportDimension>[
        _dimension('TASK_ACHIEVEMENT', 80, percentage: true),
        _dimension('CLARITY_COHERENCE', 55, percentage: true),
        _dimension('LANGUAGE_CONTROL', 70, percentage: true),
        _dimension('INTERACTION', null, percentage: true),
      ],
      priorityActions: const <EvaluationReportPriorityAction>[
        EvaluationReportPriorityAction(
          dimensionKey: 'TASK_ACHIEVEMENT',
          findingId: 'missing',
        ),
      ],
    );

    final result = presentEvaluationReportDetail(report);

    expect(result.hasPriorityDimensions, isFalse);
    expect(result.dimensions.map((dimension) => dimension.key), <String>[
      'CLARITY_COHERENCE',
      'LANGUAGE_CONTROL',
      'TASK_ACHIEVEMENT',
      'INTERACTION',
    ]);
    expect(result.overallScoreLabel, '--');
  });

  test('suggestions are trimmed, deduplicated, and preserve source order', () {
    const evidence = EvaluationReportEvidence(
      evidenceRefId: 'evidence',
      turnId: 'turn',
      startUtf8Byte: 0,
      endUtf8Byte: 5,
      originalExcerpt: 'exact quote',
    );
    final report = _report(
      sceneType: EvaluationReportSceneType.overseasWorkplace,
      dimensions: <EvaluationReportDimension>[
        _dimension(
          'TASK_ACHIEVEMENT',
          80,
          percentage: true,
          improvements: const <EvaluationReportFinding>[
            EvaluationReportFinding(
              id: 'one',
              message: 'Finding one',
              suggestion: ' Repeat this ',
              evidence: <EvaluationReportEvidence>[evidence],
            ),
            EvaluationReportFinding(
              id: 'two',
              message: 'Finding two',
              suggestion: 'Repeat this',
              evidence: <EvaluationReportEvidence>[],
            ),
          ],
          examples: const <EvaluationReportFinding>[
            EvaluationReportFinding(
              id: 'example',
              message: 'Use this expression',
              evidence: <EvaluationReportEvidence>[],
            ),
          ],
        ),
      ],
    );

    final dimension = presentEvaluationReportDetail(report).dimensions.single;

    expect(dimension.suggestions, <String>[
      'Repeat this',
      'Use this expression',
    ]);
    expect(dimension.findings.first.evidenceExcerpts, <String>['exact quote']);
  });

  test('questions are sorted and unanswered text is retained', () {
    final report = _report(
      sceneType: EvaluationReportSceneType.interview,
      dimensions: const <EvaluationReportDimension>[],
      questions: const <EvaluationReportQuestion>[
        EvaluationReportQuestion(id: 'second', position: 2, text: 'Second?'),
        EvaluationReportQuestion(
          id: 'first',
          position: 1,
          text: 'First?',
          answer: EvaluationReportAnswer(turnId: 'turn', transcript: 'Answer'),
        ),
      ],
    );

    final questions = presentEvaluationReportDetail(report).questions;

    expect(questions.map((question) => question.id), <String>[
      'first',
      'second',
    ]);
    expect(questions.first.answerText, 'Answer');
    expect(questions.last.answerText, '未作答');
    expect(questions.last.answered, isFalse);
  });

  test('reason code maps to a safe unavailable message', () {
    final dimension = _dimension(
      'PRONUNCIATION',
      null,
      reasonCodes: const <String>['ACOUSTIC_ASSESSMENT_FAILED'],
    );

    expect(evaluationUnavailableReason(dimension), '本次发音评测失败，未使用文字推测发音。');
    expect(evaluationUnavailableReason(null), '本项没有足够的评分依据。');
  });
}

EvaluationReport _report({
  required EvaluationReportSceneType sceneType,
  required List<EvaluationReportDimension> dimensions,
  String practiceMode = 'FULL_SIMULATION',
  List<EvaluationReportQuestion> questions = const <EvaluationReportQuestion>[],
  List<EvaluationReportPriorityAction> priorityActions =
      const <EvaluationReportPriorityAction>[],
}) => EvaluationReport(
  id: 'report',
  evaluationId: 'evaluation',
  practiceSessionId: 'session',
  sceneType: sceneType,
  practiceExperience: 'experience',
  sceneCategory: 'category',
  practiceMode: practiceMode,
  scoreability: EvaluationReportScoreability.provisional,
  summary: 'summary',
  questions: questions,
  dimensions: dimensions,
  priorityActions: priorityActions,
  createdAt: DateTime.utc(2026, 8, 21),
);

EvaluationReportDimension _dimension(
  String key,
  double? score, {
  bool percentage = false,
  List<String> reasonCodes = const <String>[],
  List<EvaluationReportFinding> improvements =
      const <EvaluationReportFinding>[],
  List<EvaluationReportFinding> examples = const <EvaluationReportFinding>[],
}) => EvaluationReportDimension(
  key: key,
  score: score,
  scale: percentage
      ? EvaluationReportScoreScale.percentage100
      : EvaluationReportScoreScale.ieltsBand,
  coverage: score == null ? 0 : 1,
  confidence: score == null ? 0 : 0.8,
  reasonCodes: reasonCodes,
  evidenceRefIds: const <String>[],
  strengths: const <EvaluationReportFinding>[],
  improvements: improvements,
  recommendedExamples: examples,
);
