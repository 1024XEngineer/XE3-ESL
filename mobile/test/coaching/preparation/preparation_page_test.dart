import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/preparation/preparation.dart';
import 'package:speakup/features/coaching/preparation/preparation_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_client.dart';

import '../../support/scene_fixtures.dart';

void main() {
  testWidgets('english interview is a plan library with one create action', (
    tester,
  ) async {
    final controller = PreparationController(client: _FixtureClient());
    addTearDown(controller.dispose);
    var creates = 0;

    await tester.pumpWidget(
      MaterialApp(
        home: PreparationPage(
          preparationController: controller,
          onOpenJobPreparation: () => creates++,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('practice-hub-interview')));
    await tester.pumpAndSettle();

    expect(find.text('创建并管理你的模拟面试。'), findsOneWidget);
    expect(find.byKey(const Key('create-interview-plan')), findsOneWidget);
    expect(find.text('专项练习'), findsNothing);
    expect(find.text('开始模拟面试'), findsNothing);

    await tester.tap(find.byKey(const Key('create-interview-plan')));
    expect(creates, 1);
  });

  testWidgets('training center keeps four product directions', (tester) async {
    final controller = PreparationController(client: _FourFamilyClient());
    addTearDown(controller.dispose);
    await tester.pumpWidget(
      MaterialApp(home: PreparationPage(preparationController: controller)),
    );
    await tester.pumpAndSettle();

    for (final hub in const [
      'practice-hub-interview',
      'practice-hub-exam',
      'practice-hub-workplace',
      'practice-hub-life',
    ]) {
      final finder = find.byKey(Key(hub));
      await tester.scrollUntilVisible(
        finder,
        160,
        scrollable: find.byType(Scrollable).first,
      );
      expect(finder, findsOneWidget);
    }
  });

  testWidgets('scene catalog exposes loading, retry, and empty states', (
    tester,
  ) async {
    final client = _ControlledListClient();
    final controller = PreparationController(client: client);
    addTearDown(controller.dispose);
    await tester.pumpWidget(
      MaterialApp(home: PreparationPage(preparationController: controller)),
    );
    await tester.pump();
    expect(
      find.byKey(const Key('preparation-catalog-loading')),
      findsOneWidget,
    );

    client.first.completeError(
      const SceneClientException(
        kind: SceneClientFailureKind.network,
        retryable: true,
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('preparation-catalog-error')), findsOneWidget);

    await tester.tap(find.byKey(const Key('preparation-catalog-retry')));
    await tester.pump();
    client.second.complete(const <SceneDefinition>[]);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('preparation-catalog-empty')), findsOneWidget);
  });
}

final class _FixtureClient implements SceneClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) async => testScenes.first;

  @override
  Future<List<SceneDefinition>> listScenes() async => [testScenes.first];

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) async =>
      testScenes.first.roles;
}

final class _FourFamilyClient implements SceneClient {
  @override
  Future<SceneDefinition> getScene(String sceneId) async => listScenes().then(
    (scenes) => scenes.firstWhere((scene) => scene.id == sceneId),
  );

  @override
  Future<List<SceneDefinition>> listScenes() async {
    final interview = testScenes.first;
    SceneDefinition copy(String id, SceneCategory category) => SceneDefinition(
      id: id,
      version: interview.version,
      name: id,
      experience: category == SceneCategory.ieltsSpeaking
          ? PracticeExperience.ieltsSpeaking
          : category.name.startsWith('roleplay')
          ? PracticeExperience.roleplay
          : PracticeExperience.interview,
      category: category,
      roles: interview.roles,
      practiceOptions: interview.practiceOptions,
      prompt: interview.prompt,
      status: interview.status,
    );
    return [
      interview,
      copy('ielts', SceneCategory.ieltsSpeaking),
      copy('workplace', SceneCategory.roleplayWorkplace),
      copy('life', SceneCategory.roleplayDaily),
    ];
  }

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) async =>
      testScenes.first.roles;
}

final class _ControlledListClient implements SceneClient {
  final first = Completer<List<SceneDefinition>>();
  final second = Completer<List<SceneDefinition>>();
  var calls = 0;

  @override
  Future<SceneDefinition> getScene(String sceneId) =>
      throw UnimplementedError();

  @override
  Future<List<SceneDefinition>> listScenes() =>
      calls++ == 0 ? first.future : second.future;

  @override
  Future<List<RoleDefinition>> listRoles(String sceneId) =>
      throw UnimplementedError();
}
