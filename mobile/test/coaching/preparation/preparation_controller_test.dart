import '../../support/scene_fixtures.dart';
import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

void main() {
  test(
    'keeps role order optional and filters options for the selected role',
    () async {
      final controller = PreparationController(client: _FixtureCatalogClient());
      addTearDown(controller.dispose);

      await controller.loadIfNeeded();
      await controller.selectScene(_scene);

      expect(controller.roles, [_technicalRole, _recruiterRole]);
      expect(controller.selectedRole, isNull);
      expect(controller.availableOptions, isEmpty);

      controller.selectRole(_technicalRole);
      expect(controller.availableOptions.map((option) => option.id), [
        'option_full_simulation',
        'option_technical_focus',
      ]);
      controller.selectOption(_fullOption);
      expect(controller.hasCompleteSelection, isTrue);

      controller.selectRole(_recruiterRole);
      expect(controller.selectedOption, isNull);
      expect(controller.availableOptions.map((option) => option.id), [
        'option_full_simulation',
        'option_hr_focus',
      ]);
    },
  );

  test(
    'selects the interview specialty configuration without another prompt',
    () async {
      final controller = PreparationController(client: _FixtureCatalogClient());
      addTearDown(controller.dispose);

      await controller.loadIfNeeded();
      await controller.selectScene(_scene);

      expect(controller.selectRecommendedConfiguration(), isTrue);
      expect(controller.selectedRole, _technicalRole);
      expect(controller.selectedOption, _technicalFocus);
      expect(controller.hasCompleteSelection, isTrue);
    },
  );

  test('keeps the IELTS full mock entry on the FULL_MOCK option', () async {
    final controller = PreparationController(client: _IeltsCatalogClient());
    addTearDown(controller.dispose);

    await controller.loadIfNeeded();
    await controller.selectScene(_ieltsScene);

    expect(controller.selectRecommendedConfiguration(), isTrue);
    expect(controller.selectedRole, _ieltsRole);
    expect(controller.selectedOption, _ieltsFullOption);
  });

  test('keeps IELTS section practice on its complete section option', () async {
    final scene = testScene(
      id: 'scn_ielts_speaking_test',
      experience: PracticeExperience.ieltsSpeaking,
      category: SceneCategory.ieltsSpeaking,
      name: 'IELTS Speaking Part 2',
      version: 2,
      prompt: _prompt,
    );
    final role = testRole(
      id: 'role_ielts_part_2_examiner',
      sceneId: 'scn_ielts_speaking_test',
      type: 'IELTS_EXAMINER',
      displayName: 'IELTS 口语考官',
      responsibilities: 'Run Part 2 and the bound Part 3.',
      style: 'Neutral and concise.',
      practiceObjectiveIds: ['part_2', 'part_3'],
    );
    final full = testPracticeOption(
      id: 'option_ielts_part_2_full',
      sceneId: 'scn_ielts_speaking_test',
      mode: PracticeMode.part1,
      displayName: 'Part 1',
    );
    final part2 = testPracticeOption(
      id: 'option_ielts_part_2',
      sceneId: 'scn_ielts_speaking_test',
      mode: PracticeMode.part2,
      displayName: 'Part 2',
    );
    final part3 = testPracticeOption(
      id: 'option_ielts_part_3',
      sceneId: 'scn_ielts_speaking_test',
      mode: PracticeMode.part3,
      displayName: 'Part 3',
    );
    final fullMock = testPracticeOption(
      id: 'option_ielts_full_mock',
      sceneId: 'scn_ielts_speaking_test',
      mode: PracticeMode.fullMock,
      displayName: '完整模考',
    );
    final detail = testScene(
      id: scene.id,
      experience: scene.experience,
      category: scene.category,
      name: scene.name,
      version: scene.version,
      status: scene.status,
      prompt: _prompt,
      roles: [role],
      practiceOptions: [fullMock, full, part2, part3],
    );
    final controller = PreparationController(
      client: _IeltsCatalogClient(scene: scene, detail: detail, role: role),
    );
    addTearDown(controller.dispose);

    await controller.loadIfNeeded();
    await controller.selectScene(scene);

    expect(
      controller.selectRecommendedConfiguration(
        practiceMode: PracticeMode.part2,
      ),
      isTrue,
    );
    expect(controller.selectedRole, role);
    expect(controller.selectedOption, part2);
  });

  test('retries the failed directory request', () async {
    final client = _FailOnceCatalogClient();
    final controller = PreparationController(client: client);
    addTearDown(controller.dispose);

    await controller.loadIfNeeded();
    expect(controller.errorMessage, isNotNull);
    expect(controller.scenes, isEmpty);

    await controller.retryLastFailure();
    expect(controller.errorMessage, isNull);
    expect(controller.scenes, [_scene]);
    expect(client.listCalls, 2);
  });

  test(
    'account cleanup clears selection and ignores a late response',
    () async {
      final client = _ControlledCatalogClient();
      final controller = PreparationController(client: client);
      addTearDown(controller.dispose);

      final load = controller.loadIfNeeded();
      await controller.clearPrivateState();
      client.listResponse.complete([_scene]);
      await load;

      expect(controller.scenes, isEmpty);
      expect(controller.selectedScene, isNull);
      expect(controller.selectedRole, isNull);
      expect(controller.selectedOption, isNull);
      expect(controller.errorMessage, isNull);
      expect(controller.isLoadingScenes, isFalse);
    },
  );

  test('rejects a catalog that omits one role focus option', () async {
    final controller = PreparationController(
      client: _FixtureCatalogClient(
        detail: testScene(
          id: _scene.id,
          experience: _scene.experience,
          category: _scene.category,
          name: _scene.name,
          version: _scene.version,
          status: _scene.status,
          prompt: _scene.prompt,
          roles: [_technicalRole, _recruiterRole],
          practiceOptions: [_fullOption, _technicalFocus],
        ),
      ),
    );
    addTearDown(controller.dispose);

    await controller.loadIfNeeded();
    await controller.selectScene(_scene);

    expect(controller.detail, isNull);
    expect(controller.roles, isEmpty);
    expect(controller.errorMessage, '练习目录响应无法识别，请稍后重试。');
  });
}

class _FixtureCatalogClient implements SceneClient {
  _FixtureCatalogClient({SceneDefinition? detail}) : detail = detail ?? _detail;

  final SceneDefinition detail;

  @override
  Future<SceneDefinition> getScene(String sceneId) async => detail;

  @override
  Future<List<SceneDefinition>> listScenes() async => [_scene];

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) async => [
    _technicalRole,
    _recruiterRole,
  ];
}

final class _FailOnceCatalogClient extends _FixtureCatalogClient {
  int listCalls = 0;

  @override
  Future<List<SceneDefinition>> listScenes() async {
    listCalls++;
    if (listCalls == 1) {
      throw const SceneClientException(
        kind: SceneClientFailureKind.network,
        retryable: true,
      );
    }
    return [_scene];
  }
}

final class _ControlledCatalogClient implements SceneClient {
  final Completer<List<SceneDefinition>> listResponse =
      Completer<List<SceneDefinition>>();

  @override
  Future<SceneDefinition> getScene(String sceneId) {
    throw UnimplementedError();
  }

  @override
  Future<List<SceneDefinition>> listScenes() => listResponse.future;

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) {
    throw UnimplementedError();
  }
}

final class _IeltsCatalogClient implements SceneClient {
  _IeltsCatalogClient({
    SceneDefinition? scene,
    SceneDefinition? detail,
    RoleDefinition? role,
  }) : scene = scene ?? _ieltsScene,
       detail = detail ?? _ieltsDetail,
       role = role ?? _ieltsRole;

  final SceneDefinition scene;
  final SceneDefinition detail;
  final RoleDefinition role;

  @override
  Future<SceneDefinition> getScene(String sceneId) async => detail;

  @override
  Future<List<SceneDefinition>> listScenes() async => [scene];

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) async => [role];
}

const _sceneId = 'scn_programmer_interview';

final _scene = testScene(
  id: _sceneId,
  experience: PracticeExperience.interview,
  category: SceneCategory.interviewProfessional,
  name: 'English interview for technical roles',
  version: 1,
  prompt: _prompt,
);

const _prompt = ScenePrompt(
  publicSceneBrief: 'Discuss one backend project.',
  practiceGoal: 'Explain decisions with evidence.',
  userRole: 'Candidate',
  aiRole: 'Technical interviewer',
  personaSummary: 'Precise and evidence seeking.',
  focusAreas: ['system_design'],
  turnBlueprints: ['Ask for a project overview.'],
);

final _technicalRole = testRole(
  id: 'role_technical_interviewer',
  sceneId: _sceneId,
  type: 'TECHNICAL_INTERVIEWER',
  displayName: 'Technical depth perspective',
  responsibilities: 'Probe technical depth.',
  style: 'Precise and evidence seeking.',
  practiceObjectiveIds: ['system_design'],
);

final _recruiterRole = testRole(
  id: 'role_hr_interviewer',
  sceneId: _sceneId,
  type: 'HR_INTERVIEWER',
  displayName: 'Recruiter and motivation perspective',
  responsibilities: 'Explore motivation.',
  style: 'Warm and structured.',
  practiceObjectiveIds: ['motivation'],
);

final _fullOption = testPracticeOption(
  id: 'option_full_simulation',
  sceneId: _sceneId,
  mode: PracticeMode.fullSimulation,
  displayName: 'Full simulation',
);

final _technicalFocus = testPracticeOption(
  id: 'option_technical_focus',
  sceneId: _sceneId,
  roleId: 'role_technical_interviewer',
  mode: PracticeMode.focus,
  displayName: 'Technical depth focus',
);

final _recruiterFocus = testPracticeOption(
  id: 'option_hr_focus',
  sceneId: _sceneId,
  roleId: 'role_hr_interviewer',
  mode: PracticeMode.focus,
  displayName: 'Recruiter and motivation focus',
);

final _detail = testScene(
  id: _scene.id,
  experience: _scene.experience,
  category: _scene.category,
  name: _scene.name,
  version: _scene.version,
  status: _scene.status,
  prompt: _scene.prompt,
  roles: [_technicalRole, _recruiterRole],
  practiceOptions: [_fullOption, _technicalFocus, _recruiterFocus],
);

const _ieltsSceneId = 'scn_ielts_speaking_test';

final _ieltsScene = testScene(
  id: _ieltsSceneId,
  experience: PracticeExperience.ieltsSpeaking,
  category: SceneCategory.ieltsSpeaking,
  name: 'IELTS 口语完整模拟',
  version: 2,
  prompt: _prompt,
);

final _ieltsRole = testRole(
  id: 'role_ielts_examiner',
  sceneId: _ieltsSceneId,
  type: 'IELTS_EXAMINER',
  displayName: 'IELTS 口语考官',
  responsibilities: 'Run the complete mock test.',
  style: 'Neutral and concise.',
  practiceObjectiveIds: ['part_1', 'part_2', 'part_3'],
);

final _ieltsFullOption = testPracticeOption(
  id: 'option_ielts_speaking_full_full',
  sceneId: _ieltsSceneId,
  mode: PracticeMode.fullMock,
  displayName: '完整模考',
);

final _ieltsPart1Option = testPracticeOption(
  id: 'option_ielts_speaking_part_1',
  sceneId: _ieltsSceneId,
  mode: PracticeMode.part1,
  displayName: 'Part 1',
);

final _ieltsPart2Option = testPracticeOption(
  id: 'option_ielts_speaking_part_2',
  sceneId: _ieltsSceneId,
  mode: PracticeMode.part2,
  displayName: 'Part 2',
);

final _ieltsPart3Option = testPracticeOption(
  id: 'option_ielts_speaking_part_3',
  sceneId: _ieltsSceneId,
  mode: PracticeMode.part3,
  displayName: 'Part 3',
);

final _ieltsDetail = testScene(
  id: _ieltsScene.id,
  experience: _ieltsScene.experience,
  category: _ieltsScene.category,
  name: _ieltsScene.name,
  version: _ieltsScene.version,
  status: _ieltsScene.status,
  prompt: _ieltsScene.prompt,
  roles: [_ieltsRole],
  practiceOptions: [
    _ieltsFullOption,
    _ieltsPart1Option,
    _ieltsPart2Option,
    _ieltsPart3Option,
  ],
);
