import 'package:speakup/features/coaching/goal/goal.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

const _prompt = ScenePrompt(
  publicSceneBrief: '围绕真实经历完成一轮英语练习。',
  practiceGoal: '清楚表达背景、行动与结果。',
  userRole: 'Learner',
  aiRole: 'Coach',
  personaSummary: 'Structured and evidence seeking.',
  focusAreas: <String>['clarity'],
  turnBlueprints: <String>['Ask for one concrete example.'],
  suggestedDurationSeconds: 600,
);

const testScenes = <SceneDefinition>[
  SceneDefinition(
    id: 'self-introduction',
    family: SceneFamily.interview,
    model: SceneModel.interviewBasicDialogue,
    name: '英文自我介绍',
    version: 1,
    status: SceneStatus.active,
    turnPolicyRef: 'turn-policy-interview',
    sessionPolicyRef: 'session-policy-interview',
    evaluationPolicyRef: 'evaluation-policy-interview',
    prompt: _prompt,
    roles: <RoleDefinition>[
      RoleDefinition(
        id: 'role-self-introduction',
        sceneId: 'self-introduction',
        type: 'INTERVIEWER',
        displayName: 'Interviewer',
        responsibilities: 'Guide the introduction.',
        style: 'Structured.',
        practiceObjectives: <RolePracticeObjective>[
          RolePracticeObjective(
            objectiveId: 'clarity',
            description: 'Communicate clearly.',
          ),
        ],
      ),
    ],
    practiceOptions: <PracticeOption>[
      PracticeOption(
        id: 'option-self-introduction',
        sceneId: 'self-introduction',
        type: PracticeOptionType.fullSimulation,
        displayName: '完整练习',
      ),
    ],
  ),
  SceneDefinition(
    id: 'behavioral-interview',
    family: SceneFamily.interview,
    model: SceneModel.interviewBasicDialogue,
    name: '行为面试',
    version: 1,
    status: SceneStatus.active,
    turnPolicyRef: 'turn-policy-interview',
    sessionPolicyRef: 'session-policy-interview',
    evaluationPolicyRef: 'evaluation-policy-interview',
    prompt: _prompt,
    roles: <RoleDefinition>[
      RoleDefinition(
        id: 'role-behavioral-interview',
        sceneId: 'behavioral-interview',
        type: 'INTERVIEWER',
        displayName: 'Interviewer',
        responsibilities: 'Probe behavioral examples.',
        style: 'Structured.',
        practiceObjectives: <RolePracticeObjective>[
          RolePracticeObjective(
            objectiveId: 'clarity',
            description: 'Communicate clearly.',
          ),
        ],
      ),
    ],
    practiceOptions: <PracticeOption>[
      PracticeOption(
        id: 'option-behavioral-interview',
        sceneId: 'behavioral-interview',
        type: PracticeOptionType.fullSimulation,
        displayName: '完整练习',
      ),
    ],
  ),
  SceneDefinition(
    id: 'project-deep-dive',
    family: SceneFamily.interview,
    model: SceneModel.projectExperienceDeepDive,
    name: '项目经历深挖',
    version: 1,
    status: SceneStatus.active,
    turnPolicyRef: 'turn-policy-project',
    sessionPolicyRef: 'session-policy-project',
    evaluationPolicyRef: 'evaluation-policy-project',
    prompt: _prompt,
    roles: <RoleDefinition>[
      RoleDefinition(
        id: 'role-project-deep-dive',
        sceneId: 'project-deep-dive',
        type: 'TECHNICAL_INTERVIEWER',
        displayName: 'Technical interviewer',
        responsibilities: 'Probe project decisions.',
        style: 'Precise.',
        practiceObjectives: <RolePracticeObjective>[
          RolePracticeObjective(
            objectiveId: 'clarity',
            description: 'Communicate clearly.',
          ),
        ],
      ),
    ],
    practiceOptions: <PracticeOption>[
      PracticeOption(
        id: 'option-project-deep-dive',
        sceneId: 'project-deep-dive',
        type: PracticeOptionType.fullSimulation,
        displayName: '完整练习',
      ),
    ],
  ),
  SceneDefinition(
    id: 'technical-qa',
    family: SceneFamily.interview,
    model: SceneModel.interviewBasicDialogue,
    name: '技术问答',
    version: 1,
    status: SceneStatus.active,
    turnPolicyRef: 'turn-policy-technical',
    sessionPolicyRef: 'session-policy-technical',
    evaluationPolicyRef: 'evaluation-policy-technical',
    prompt: _prompt,
    roles: <RoleDefinition>[
      RoleDefinition(
        id: 'role-technical-qa',
        sceneId: 'technical-qa',
        type: 'TECHNICAL_INTERVIEWER',
        displayName: 'Technical interviewer',
        responsibilities: 'Ask technical questions.',
        style: 'Concise.',
        practiceObjectives: <RolePracticeObjective>[
          RolePracticeObjective(
            objectiveId: 'clarity',
            description: 'Communicate clearly.',
          ),
        ],
      ),
    ],
    practiceOptions: <PracticeOption>[
      PracticeOption(
        id: 'option-technical-qa',
        sceneId: 'technical-qa',
        type: PracticeOptionType.fullSimulation,
        displayName: '完整练习',
      ),
    ],
  ),
];

SceneDefinition testScene({
  String id = 'scene-test',
  SceneFamily family = SceneFamily.interview,
  SceneModel model = SceneModel.interviewBasicDialogue,
  String name = 'Test scene',
  int version = 1,
  SceneStatus status = SceneStatus.active,
  String? turnPolicyRef,
  String? sessionPolicyRef,
  String? evaluationPolicyRef,
  ScenePrompt? prompt,
  List<RoleDefinition>? roles,
  List<PracticeOption>? practiceOptions,
}) {
  final resolvedPrompt = prompt ?? _prompt;
  final resolvedRoles = roles ?? const <RoleDefinition>[];
  final completeRoles = resolvedRoles.isEmpty
      ? <RoleDefinition>[
          RoleDefinition(
            id: 'role-$id',
            sceneId: id,
            type: 'COACH',
            displayName: 'Coach',
            responsibilities: 'Guide the test practice.',
            style: 'Structured.',
            practiceObjectives: const <RolePracticeObjective>[
              RolePracticeObjective(
                objectiveId: 'clarity',
                description: 'Communicate clearly.',
              ),
            ],
          ),
        ]
      : resolvedRoles;
  final resolvedOptions = practiceOptions ?? const <PracticeOption>[];
  final completeOptions = resolvedOptions.isEmpty
      ? <PracticeOption>[
          PracticeOption(
            id: 'option-$id',
            sceneId: id,
            type: PracticeOptionType.fullSimulation,
            displayName: 'Full simulation',
          ),
        ]
      : resolvedOptions;
  return SceneDefinition(
    id: id,
    family: family,
    model: model,
    name: name,
    version: version,
    status: status,
    turnPolicyRef: turnPolicyRef ?? 'turn-$id',
    sessionPolicyRef: sessionPolicyRef ?? 'session-$id',
    evaluationPolicyRef: evaluationPolicyRef ?? 'evaluation-$id',
    prompt: resolvedPrompt,
    roles: List<RoleDefinition>.unmodifiable(completeRoles),
    practiceOptions: List<PracticeOption>.unmodifiable(completeOptions),
  );
}

RoleDefinition testRole({
  required String id,
  required String sceneId,
  required String type,
  required String displayName,
  required String responsibilities,
  required String style,
  required List<String> practiceObjectiveIds,
  String? voiceConfigRef,
}) => RoleDefinition(
  id: id,
  sceneId: sceneId,
  type: type,
  displayName: displayName,
  responsibilities: responsibilities,
  style: style,
  practiceObjectives: practiceObjectiveIds
      .map(
        (objectiveId) => RolePracticeObjective(
          objectiveId: objectiveId,
          description: 'Practice $objectiveId.',
        ),
      )
      .toList(growable: false),
  voiceConfigRef: voiceConfigRef,
);

PracticeOption testPracticeOption({
  required String id,
  required String sceneId,
  required PracticeOptionType type,
  required String displayName,
  String? roleId,
}) => PracticeOption(
  id: id,
  sceneId: sceneId,
  type: type,
  displayName: displayName,
  roleId: roleId,
);

Goal testGoal({
  String id = 'goal-1',
  String title = '练习目标',
  GoalStatus status = GoalStatus.active,
  int version = 1,
  DateTime? createdAt,
  DateTime? updatedAt,
}) {
  final timestamp = createdAt ?? DateTime.utc(2026, 1, 1);
  return Goal(
    id: id,
    title: title,
    status: status,
    version: version,
    createdAt: timestamp,
    updatedAt: updatedAt ?? timestamp,
  );
}
