import 'package:speakup/features/coaching/scene/scene.dart';

const _prompt = ScenePrompt(
  publicSceneBrief: '围绕真实经历完成一轮英语练习。',
  practiceGoal: '清楚表达背景、行动与结果。',
  userRole: 'Learner',
  aiRole: 'Coach',
  personaSummary: 'Structured and evidence seeking.',
  focusAreas: <String>['clarity'],
  turnBlueprints: <String>['Ask for one concrete example.'],
);

const testScenes = <SceneDefinition>[
  SceneDefinition(
    id: 'self-introduction',
    experience: PracticeExperience.interview,
    category: SceneCategory.interviewRecruiter,
    name: '英文自我介绍',
    version: 1,
    status: SceneStatus.active,
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
        mode: PracticeMode.fullSimulation,
        displayName: '完整练习',
        suggestedDurationSeconds: 600,
        turnPolicyRef: 'turn-policy-interview',
        sessionPolicyRef: 'session-policy-interview',
        evaluationPolicyRef: 'evaluation-policy-interview',
      ),
    ],
  ),
  SceneDefinition(
    id: 'behavioral-interview',
    experience: PracticeExperience.interview,
    category: SceneCategory.interviewBehavioral,
    name: '行为面试',
    version: 1,
    status: SceneStatus.active,
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
        mode: PracticeMode.fullSimulation,
        displayName: '完整练习',
        suggestedDurationSeconds: 600,
        turnPolicyRef: 'turn-policy-interview',
        sessionPolicyRef: 'session-policy-interview',
        evaluationPolicyRef: 'evaluation-policy-interview',
      ),
    ],
  ),
  SceneDefinition(
    id: 'project-deep-dive',
    experience: PracticeExperience.interview,
    category: SceneCategory.interviewProfessional,
    name: '项目经历深挖',
    version: 1,
    status: SceneStatus.active,
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
        mode: PracticeMode.fullSimulation,
        displayName: '完整练习',
        suggestedDurationSeconds: 600,
        turnPolicyRef: 'turn-policy-project',
        sessionPolicyRef: 'session-policy-project',
        evaluationPolicyRef: 'evaluation-policy-project',
      ),
    ],
  ),
  SceneDefinition(
    id: 'technical-qa',
    experience: PracticeExperience.interview,
    category: SceneCategory.interviewProfessional,
    name: '技术问答',
    version: 1,
    status: SceneStatus.active,
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
        mode: PracticeMode.fullSimulation,
        displayName: '完整练习',
        suggestedDurationSeconds: 600,
        turnPolicyRef: 'turn-policy-technical',
        sessionPolicyRef: 'session-policy-technical',
        evaluationPolicyRef: 'evaluation-policy-technical',
      ),
    ],
  ),
];

SceneDefinition testScene({
  String id = 'scene-test',
  PracticeExperience experience = PracticeExperience.interview,
  SceneCategory category = SceneCategory.interviewRecruiter,
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
            mode: PracticeMode.fullSimulation,
            displayName: 'Full simulation',
            suggestedDurationSeconds: 600,
            turnPolicyRef: turnPolicyRef ?? 'turn-$id',
            sessionPolicyRef: sessionPolicyRef ?? 'session-$id',
            evaluationPolicyRef: evaluationPolicyRef ?? 'evaluation-$id',
          ),
        ]
      : resolvedOptions;
  return SceneDefinition(
    id: id,
    experience: experience,
    category: category,
    name: name,
    version: version,
    status: status,
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
  required PracticeMode mode,
  required String displayName,
  String? roleId,
  int suggestedDurationSeconds = 600,
  String? turnPolicyRef,
  String? sessionPolicyRef,
  String? evaluationPolicyRef,
}) => PracticeOption(
  id: id,
  sceneId: sceneId,
  mode: mode,
  displayName: displayName,
  roleId: roleId,
  suggestedDurationSeconds: suggestedDurationSeconds,
  turnPolicyRef: turnPolicyRef ?? 'turn-$id',
  sessionPolicyRef: sessionPolicyRef ?? 'session-$id',
  evaluationPolicyRef: evaluationPolicyRef ?? 'evaluation-$id',
);
