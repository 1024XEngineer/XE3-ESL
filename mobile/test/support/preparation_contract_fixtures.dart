import 'dart:convert';

import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_wire_codec.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_wire_codec.dart';

const contractUserId = '11111111-1111-4111-8111-111111111111';
const contractPlanId = '22222222-2222-4222-8222-222222222222';
const contractThreadId = '33333333-3333-4333-8333-333333333333';
const contractSessionId = '44444444-4444-4444-8444-444444444444';
const contractInterviewId = '55555555-5555-4555-8555-555555555555';
const contractCreatedAt = '2026-08-15T00:00:00Z';
const contractBackground =
    'Backend engineer preparing for a system design interview.';

final SceneDefinition contractScene = SceneDefinition(
  id: 'project-deep-dive',
  experience: PracticeExperience.interview,
  category: SceneCategory.interviewProfessional,
  name: 'Project deep dive',
  version: 1,
  status: SceneStatus.active,
  prompt: const ScenePrompt(
    publicSceneBrief: 'Discuss one real project.',
    practiceGoal: 'Explain decisions and trade-offs clearly.',
    userRole: 'Candidate',
    aiRole: 'Interviewer',
    personaSummary: 'Evidence seeking technical interviewer.',
    focusAreas: <String>['clarity'],
    turnBlueprints: <String>['Describe one project.'],
  ),
  roles: const <RoleDefinition>[
    RoleDefinition(
      id: 'technical-interviewer',
      sceneId: 'project-deep-dive',
      type: 'INTERVIEWER',
      displayName: 'Technical interviewer',
      responsibilities: 'Probe engineering decisions.',
      style: 'Direct and structured.',
      practiceObjectives: <RolePracticeObjective>[
        RolePracticeObjective(
          objectiveId: 'clarity',
          description: 'Communicate clearly.',
        ),
      ],
    ),
  ],
  practiceOptions: const <PracticeOption>[
    PracticeOption(
      id: 'full-simulation',
      sceneId: 'project-deep-dive',
      mode: PracticeMode.fullSimulation,
      displayName: 'Full simulation',
      suggestedDurationSeconds: 600,
      turnPolicyRef: 'interview-turn-policy',
      sessionPolicyRef: 'interview-session-policy',
      evaluationPolicyRef: 'interview-evaluation-policy',
    ),
  ],
);

const contractInterviewInput = InterviewPreparationInput(
  source: InterviewPreparationSource.jobDescription,
  jobDescription: 'Build reliable Go services and communicate trade-offs.',
  candidateBackground: 'Backend engineer.',
  practiceFocus: 'System design decisions.',
);

const contractCandidate = InterviewPreparationCandidate(
  source: InterviewPreparationSource.jobDescription,
  generalAdviceOnly: false,
  jobTitle: 'Backend engineer',
  company: 'Example Co',
  seniority: 'Senior',
  responsibilities: <String>['Design reliable services'],
  coreSkills: <String>['Go', 'PostgreSQL'],
  communicationFocus: <String>['Trade-offs'],
  practiceGoals: <String>['Explain one architecture decision'],
  scopeNotice: 'Practice is based on the supplied role description.',
  catalogRecommendation: InterviewCatalogRecommendation(
    sceneId: 'project-deep-dive',
    sceneVersion: 1,
    selectedRoleIds: <String>['technical-interviewer'],
    practiceOptionId: 'full-simulation',
  ),
);

Map<String, Object?> contractResumeMaterialJson() => <String, Object?>{
  'target_position': 'Backend engineer',
  'professional_summary': 'Builds reliable backend systems.',
  'work_experiences': <Object?>[],
  'project_experiences': <Object?>[],
  'education_experiences': <Object?>[],
  'skills': <String>['Go', 'PostgreSQL'],
  'awards': <String>[],
};

Map<String, Object?> contractInterviewPreparationJson({
  InterviewPreparationStatus status = InterviewPreparationStatus.draft,
  int version = 1,
}) => <String, Object?>{
  'interview_preparation_id': contractInterviewId,
  'user_id': contractUserId,
  'input': encodeInterviewPreparationInput(contractInterviewInput),
  'candidate': encodeInterviewPreparationCandidate(contractCandidate),
  'resume_content': contractResumeMaterialJson(),
  'status': status.name,
  'version': version,
  'created_at': contractCreatedAt,
  'updated_at': contractCreatedAt,
};

InterviewPreparation contractInterviewPreparation({
  InterviewPreparationStatus status = InterviewPreparationStatus.draft,
  int version = 1,
}) => decodeInterviewPreparation(
  contractInterviewPreparationJson(status: status, version: version),
);

Map<String, Object?> contractPlanJson({
  PracticePlanStatus status = PracticePlanStatus.ready,
  int version = 1,
  String? sourceThreadId,
  bool includeInterview = false,
}) => <String, Object?>{
  'practice_plan_id': contractPlanId,
  'user_id': contractUserId,
  'source_thread_id': ?sourceThreadId,
  'preparation_snapshot': <String, Object?>{
    'background_summary': contractBackground,
    if (includeInterview)
      'interview': <String, Object?>{
        'interview_preparation_id': contractInterviewId,
        'version': 2,
        'input': encodeInterviewPreparationInput(contractInterviewInput),
        'candidate': encodeInterviewPreparationCandidate(contractCandidate),
        'resume_content': contractResumeMaterialJson(),
      },
  },
  'scene_selection': <String, Object?>{
    'scene': encodeSceneDefinition(contractScene),
    'selected_role_ids': <String>['technical-interviewer'],
    'practice_option_id': 'full-simulation',
  },
  'session_policy': contractSessionPolicyJson(),
  'practice_objectives': <Object?>[
    <String, Object?>{
      'objective_id': 'clarity',
      'description': 'Communicate clearly.',
    },
  ],
  'version': version,
  'practice_plan_status': status.name,
  'created_at': contractCreatedAt,
  'updated_at': contractCreatedAt,
};

Map<String, Object?> contractSessionPolicyJson() => <String, Object?>{
  'completion_mode': 'TURN_LIMITED',
  'suggested_duration_seconds': 600,
  'min_effective_turns': 3,
  'max_effective_turns': 6,
  'coverage_checkpoint_turn': 3,
  'max_follow_ups_per_question': 1,
  'early_completion_rule': 'COVERAGE_SATISFIED_AFTER_CHECKPOINT',
  'retry_allowed': true,
  'question_translation_allowed': true,
  'question_tips_allowed': true,
  'speech_feedback_allowed': true,
};

PracticePlan contractPlan({
  PracticePlanStatus status = PracticePlanStatus.ready,
  int version = 1,
  String? sourceThreadId,
  bool includeInterview = false,
}) => decodePracticePlan(
  contractPlanJson(
    status: status,
    version: version,
    sourceThreadId: sourceThreadId,
    includeInterview: includeInterview,
  ),
);

Map<String, Object?> contractBootstrapJson(PracticePlan plan) =>
    <String, Object?>{
      'practice_session': <String, Object?>{
        'practice_session_id': contractSessionId,
        'practice_plan_id': plan.id,
        'plan_version': plan.version,
        'practice_experience': plan.sceneSelection.scene.experience.wireValue,
        'scene_category': plan.sceneSelection.scene.category.wireValue,
        'practice_mode': plan.practiceOption.mode.wireValue,
        'evaluation_policy_ref': plan.practiceOption.evaluationPolicyRef,
        'practice_session_status': 'starting',
        'session_version': 1,
        'created_at': contractCreatedAt,
      },
      'snapshot': <String, Object?>{
        'practice_session_id': contractSessionId,
        'plan_version': plan.version,
        'practice_experience': plan.sceneSelection.scene.experience.wireValue,
        'scene_category': plan.sceneSelection.scene.category.wireValue,
        'practice_mode': plan.practiceOption.mode.wireValue,
        'scene_selection': <String, Object?>{
          'scene': encodeSceneDefinition(plan.sceneSelection.scene),
          'selected_role_ids': plan.sceneSelection.selectedRoleIds,
          'practice_option_id': plan.sceneSelection.practiceOptionId,
        },
        'preparation_snapshot': contractPlanJson(
          status: plan.status,
          version: plan.version,
          sourceThreadId: plan.sourceThreadId,
          includeInterview: plan.preparationSnapshot.interview != null,
        )['preparation_snapshot'],
        'participants': <Object?>[],
        'session_policy': contractSessionPolicyJson(),
        'practice_objectives': <Object?>[
          <String, Object?>{
            'objective_id': 'clarity',
            'description': 'Communicate clearly.',
          },
        ],
      },
    };

PreparationPracticeBootstrap contractBootstrap(PracticePlan plan) =>
    decodePreparationPracticeBootstrapBody(
      jsonEncode(contractBootstrapJson(plan)),
      expectedPlan: plan,
    );
