import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/review/formal_review.dart';
import 'package:speakup/review/formal_review_decoder.dart';

void main() {
  test('decodes the current legacy v1 wire result', () {
    final review = decodeFormalReview(_legacyReview());

    expect(review.schema, FormalReviewSchema.legacyVoiceV1);
    expect(review.contextType, isNull);
    expect(review.result?.eligibility, FormalReviewSummaryEligibility.eligible);
    expect(review.result?.overallScore, 87);
    expect(review.result?.dimensions.single.score, isNull);
    expect(review.result?.feedbackItems, isEmpty);
  });

  test('decodes stored legacy v1 results without summary eligibility', () {
    final value = _legacyReview();
    _result(value).remove('summary_eligibility');

    final review = decodeFormalReview(value);

    expect(review.result?.eligibility, FormalReviewSummaryEligibility.eligible);
  });

  test('decodes eligible scene dimensions, feedback, and repractice', () {
    final review = decodeFormalReview(
      _sceneReview(
        contextType: 'interview.project_deep_dive',
        result: _eligibleResult(),
      ),
    );

    expect(
      review.contextType,
      FormalReviewContextType.interviewProjectDeepDive,
    );
    expect(review.result?.overallScore, isNull);
    expect(review.result?.dimensions.single.score, 82);
    expect(
      review.result?.feedbackItems.single.kind,
      FormalReviewFeedbackKind.improvement,
    );
    expect(review.result?.repracticeSuggestionRefs, ['feedback-impact']);
  });

  test('decodes transcript-only IELTS as provisional without Overall', () {
    final review = decodeFormalReview(
      _sceneReview(
        contextType: 'ielts.speaking_part2',
        result: _provisionalResult(),
      ),
    );

    expect(
      review.result?.eligibility,
      FormalReviewSummaryEligibility.provisional,
    );
    expect(review.result?.overallScore, isNull);
    expect(review.result?.insufficientEvidenceReasons, [
      'pronunciation_audio_evidence_unavailable',
    ]);
  });

  test('decodes eligible IELTS only with a server-published Overall', () {
    final review = decodeFormalReview(
      _sceneReview(
        contextType: 'ielts.speaking_part2',
        result: {..._eligibleResult(), 'overall_score': 82},
      ),
    );

    expect(review.result?.overallScore, 82);
    expect(review.result?.eligibility, FormalReviewSummaryEligibility.eligible);
  });

  test('decodes insufficient evidence without dimensions or a zero score', () {
    final review = decodeFormalReview(
      _sceneReview(
        contextType: 'daily.hotel_checkin_issue',
        result: _insufficientResult(),
      ),
    );

    expect(
      review.result?.eligibility,
      FormalReviewSummaryEligibility.insufficientEvidence,
    );
    expect(review.result?.overallScore, isNull);
    expect(review.result?.dimensions, isEmpty);
    expect(review.result?.feedbackItems, isEmpty);
  });

  test('rejects non-eligible legacy v1 summary eligibility', () {
    for (final eligibility in <Object?>[
      'provisional',
      'insufficient_evidence',
      'unknown',
      null,
    ]) {
      final value = _legacyReview();
      _result(value)['summary_eligibility'] = eligibility;

      expect(() => decodeFormalReview(value), throwsA(_decodeFailure));
    }
  });

  test('rejects missing, mismatched, or unknown evaluation context data', () {
    final cases = <Map<String, Object?>>[
      _mutateScene((value) => value.remove('evaluation_context')),
      _mutateScene(
        (value) => value['evaluation_context_type'] = 'ielts.speaking_part2',
      ),
      _mutateScene((value) {
        _context(value)['unknown'] = true;
      }),
      _mutateScene((value) {
        _sceneContext(value)['type'] = 'generic.practice';
      }),
    ];

    for (final value in cases) {
      expect(() => decodeFormalReview(value), throwsA(_decodeFailure));
    }
  });

  test('rejects unknown enums and fields at every typed result boundary', () {
    final cases = <Map<String, Object?>>[
      _mutateScene((value) => value['status'] = 'done'),
      _mutateScene(
        (value) => _result(value)['summary_eligibility'] = 'estimated',
      ),
      _mutateScene((value) {
        _feedback(value).first['kind'] = 'compliment';
      }),
      _mutateScene((value) => _result(value)['unknown'] = true),
      _mutateScene((value) {
        _dimensions(value).first['unknown'] = true;
      }),
      _mutateScene((value) {
        _feedback(value).first['unknown'] = true;
      }),
    ];

    for (final value in cases) {
      expect(() => decodeFormalReview(value), throwsA(_decodeFailure));
    }
  });

  test('rejects duplicate item keys and invalid repractice references', () {
    final duplicateDimension = _mutateScene((value) {
      _dimensions(value).add(Map<String, Object?>.from(_dimensions(value)[0]));
    });
    final duplicateFeedback = _mutateScene((value) {
      _feedback(value).add(Map<String, Object?>.from(_feedback(value)[0]));
    });
    final danglingReference = _mutateScene((value) {
      _result(value)['repractice_suggestion_refs'] = ['missing-feedback'];
    });
    final duplicateReference = _mutateScene((value) {
      _result(value)['repractice_suggestion_refs'] = [
        'feedback-impact',
        'feedback-impact',
      ];
    });
    final paddedFeedbackKey = _mutateScene((value) {
      _feedback(value).first['key'] = ' feedback-impact';
      _result(value)['repractice_suggestion_refs'] = [' feedback-impact'];
    });

    for (final value in <Map<String, Object?>>[
      duplicateDimension,
      duplicateFeedback,
      danglingReference,
      duplicateReference,
      paddedFeedbackKey,
    ]) {
      expect(() => decodeFormalReview(value), throwsA(_decodeFailure));
    }
  });

  test('rejects out-of-range dimension and Overall scores', () {
    final cases = <Map<String, Object?>>[
      _mutateScene((value) => _dimensions(value).first['score'] = -1),
      _mutateScene((value) => _dimensions(value).first['score'] = 101),
      _sceneReview(
        contextType: 'ielts.speaking_part2',
        result: {..._eligibleResult(), 'overall_score': 101},
      ),
    ];

    for (final value in cases) {
      expect(() => decodeFormalReview(value), throwsA(_decodeFailure));
    }
  });

  test('rejects non-IELTS Overall and mutually exclusive result shapes', () {
    final cases = <Map<String, Object?>>[
      _mutateScene((value) => _result(value)['overall_score'] = 82),
      _sceneReview(
        contextType: 'ielts.speaking_part2',
        result: {..._provisionalResult(), 'overall_score': 70},
      ),
      _sceneReview(
        contextType: 'ielts.speaking_part2',
        result: _eligibleResult(),
      ),
      _sceneReview(
        contextType: 'interview.project_deep_dive',
        result: _provisionalResult(),
      ),
      _sceneReview(
        contextType: 'daily.hotel_checkin_issue',
        result: {
          ..._insufficientResult(),
          'conclusions': [_dimension()],
        },
      ),
      _sceneReview(
        contextType: 'interview.project_deep_dive',
        result: {
          ..._eligibleResult(),
          'insufficient_evidence_reasons': ['unexpected'],
        },
      ),
      _mutateScene((value) {
        _dimensions(value).first.remove('score');
      }),
    ];

    for (final value in cases) {
      expect(() => decodeFormalReview(value), throwsA(_decodeFailure));
    }
  });

  test('rejects a completed/non-completed payload state conflict', () {
    final pendingWithResult = _mutateScene((value) {
      value['status'] = 'pending';
    });
    final completedWithoutResult = _mutateScene((value) {
      value.remove('result');
    });

    expect(
      () => decodeFormalReview(pendingWithResult),
      throwsA(_decodeFailure),
    );
    expect(
      () => decodeFormalReview(completedWithoutResult),
      throwsA(_decodeFailure),
    );
  });
}

Map<String, Object?> _legacyReview() {
  return <String, Object?>{
    'review_id': 'review-v1',
    'practice_session_id': 'session-v1',
    'status': 'completed',
    'implementation_version': 'qianwen-voice-review-v1',
    'source_turn_id': 'turn-v1',
    'source_turn_version': 'conversation-turn:evidence-v1',
    'result': <String, Object?>{
      'summary_eligibility': 'eligible',
      'overall_score': 87,
      'summary': 'Clear and focused.',
      'conclusions': <Object?>[
        <String, Object?>{
          'key': 'clarity',
          'category': 'clarity',
          'message': 'The answer is easy to follow.',
          'suggestion': 'Add one measurable result.',
        },
      ],
    },
    'created_at': '2026-07-30T08:00:00Z',
    'updated_at': '2026-07-30T08:00:01Z',
    'completed_at': '2026-07-30T08:00:01Z',
  };
}

Map<String, Object?> _sceneReview({
  required String contextType,
  required Map<String, Object?> result,
}) {
  return <String, Object?>{
    'review_id': 'review-v2',
    'practice_session_id': 'session-v2',
    'status': 'completed',
    'implementation_version': 'qianwen-scene-review-v2',
    'source_turn_id': 'turn-v2',
    'source_turn_version': 'conversation-turn:evidence-v2',
    'evaluation_context_type': contextType,
    'evaluation_context': _evaluationContext(contextType),
    'result': result,
    'created_at': '2026-07-30T08:00:00Z',
    'updated_at': '2026-07-30T08:00:01Z',
    'completed_at': '2026-07-30T08:00:01Z',
  };
}

Map<String, Object?> _eligibleResult() {
  return <String, Object?>{
    'summary_eligibility': 'eligible',
    'summary': 'The answer addresses the task.',
    'conclusions': <Object?>[_dimension()],
    'feedback_items': <Object?>[
      <String, Object?>{
        'key': 'feedback-impact',
        'kind': 'improvement',
        'message': 'The impact could be more specific.',
        'suggestion': 'Add one measurable outcome.',
      },
    ],
    'repractice_suggestion_refs': <Object?>['feedback-impact'],
  };
}

Map<String, Object?> _provisionalResult() {
  return <String, Object?>{
    'summary_eligibility': 'provisional',
    'summary': 'Text-only IELTS feedback.',
    'conclusions': <Object?>[_dimension()],
    'feedback_items': <Object?>[],
    'repractice_suggestion_refs': <Object?>[],
    'insufficient_evidence_reasons': <Object?>[
      'pronunciation_audio_evidence_unavailable',
    ],
  };
}

Map<String, Object?> _insufficientResult() {
  return <String, Object?>{
    'summary_eligibility': 'insufficient_evidence',
    'summary': 'Complete another response before requesting a review.',
    'conclusions': <Object?>[],
    'insufficient_evidence_reasons': <Object?>['confirmed_answer_too_short'],
  };
}

Map<String, Object?> _dimension() {
  return <String, Object?>{
    'key': 'dimension-1',
    'category': 'language_clarity',
    'score': 82,
    'message': 'The answer is clear.',
    'suggestion': 'Use one more concrete example.',
  };
}

Map<String, Object?> _evaluationContext(String contextType) {
  final details = switch (contextType) {
    'interview.project_deep_dive' => <String, Object?>{
      'version': 'interview.project_deep_dive.v1',
      'project_brief': 'Payments migration',
      'candidate_role': 'Backend engineer',
      'focus_points': <Object?>['trade-offs'],
    },
    'ielts.speaking_part2' => <String, Object?>{
      'version': 'ielts.speaking_part2.v1',
      'cue_card_topic': 'Describe a useful object.',
      'cue_card_points': <Object?>['what it is'],
      'strict_simulation': true,
    },
    'workplace.progress_risk_update' => <String, Object?>{
      'version': 'workplace.progress_risk_update.v1',
      'initiative_brief': 'Release the new checkout.',
      'audience': 'Project sponsor',
      'expected_sections': <Object?>['progress', 'risk'],
    },
    'daily.hotel_checkin_issue' => <String, Object?>{
      'version': 'daily.hotel_checkin_issue.v1',
      'reservation_brief': 'One room for two nights.',
      'issue': 'The room is not ready.',
      'desired_outcome': 'Store luggage and confirm a check-in time.',
    },
    _ => <String, Object?>{
      'version': 'generic.practice.v1',
      'practice_goal': 'Explain one idea clearly.',
    },
  };
  final field = switch (contextType) {
    'interview.project_deep_dive' => 'interview_project_deep_dive',
    'ielts.speaking_part2' => 'ielts_speaking_part2',
    'workplace.progress_risk_update' => 'workplace_progress_risk_update',
    'daily.hotel_checkin_issue' => 'daily_hotel_checkin_issue',
    _ => 'generic_practice',
  };
  return <String, Object?>{
    'schema_version': 'evaluation-context.v1',
    'context_type': contextType,
    'scene_key': 'scene-1',
    'scene_id': 'scene-1',
    'scene_version': 1,
    'practice_option_type': 'FULL_SIMULATION',
    'difficulty_ref': 'difficulty.standard.v1',
    'assistance_ref': 'assistance.none.v1',
    'turn_policy_ref': '$contextType.turn.v1',
    'session_policy_ref': '$contextType.session.v1',
    'scene_specific_context': <String, Object?>{
      'type': contextType,
      field: details,
    },
  };
}

Map<String, Object?> _mutateScene(
  void Function(Map<String, Object?> value) mutate,
) {
  final value =
      jsonDecode(
            jsonEncode(
              _sceneReview(
                contextType: 'interview.project_deep_dive',
                result: _eligibleResult(),
              ),
            ),
          )
          as Map<String, Object?>;
  mutate(value);
  return value;
}

Map<String, Object?> _result(Map<String, Object?> review) =>
    review['result']! as Map<String, Object?>;

Map<String, Object?> _context(Map<String, Object?> review) =>
    review['evaluation_context']! as Map<String, Object?>;

Map<String, Object?> _sceneContext(Map<String, Object?> review) =>
    _context(review)['scene_specific_context']! as Map<String, Object?>;

List<Map<String, Object?>> _dimensions(Map<String, Object?> review) =>
    (_result(review)['conclusions']! as List<Object?>)
        .cast<Map<String, Object?>>();

List<Map<String, Object?>> _feedback(Map<String, Object?> review) =>
    (_result(review)['feedback_items']! as List<Object?>)
        .cast<Map<String, Object?>>();

final _decodeFailure = isA<FormalReviewDecodeException>();
