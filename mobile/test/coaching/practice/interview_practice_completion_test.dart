import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/interview/interview_practice.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/practice/practice_models.dart';

import '../../support/practice_fixtures.dart';
import '../../support/scene_fixtures.dart';

void main() {
  testWidgets(
    'manual interview completion opens the shared sheet and returns to training',
    (tester) async {
      final scene = testScenes.first;
      final controller = PracticeController(
        client: FakePracticeClient(
          practiceExperience: scene.experience,
          sceneCategory: scene.category,
          completionMode: PracticeCompletionMode.userControlled,
          turnLimit: 0,
        ),
      );
      addTearDown(controller.dispose);
      await controller.activateCreatedPractice(
        scene: scene,
        sessionId: 'session-direct-interview',
        planId: testPracticePlanId('session-direct-interview'),
        practiceMode: scene.practiceOptions.first.mode,
        turnLimit: 0,
        clientOperationId: 'activate-direct-interview',
      );
      await controller.submitPracticeText('I led the migration project.');

      final navigatorKey = GlobalKey<NavigatorState>();
      await tester.pumpWidget(
        MaterialApp(
          navigatorKey: navigatorKey,
          home: const Scaffold(body: Text('Training root')),
        ),
      );
      final routeResult = navigatorKey.currentState!
          .push<CompletedPracticeRouteResult>(
            MaterialPageRoute<CompletedPracticeRouteResult>(
              builder: (_) => InterviewPracticePage(
                practiceController: controller,
                onOpenInterviewReport: (completion) async {
                  expect(
                    completion.practiceSessionId,
                    'session-direct-interview',
                  );
                  return CompletedPracticeRouteResult.returnToTraining;
                },
              ),
            ),
          );
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('interview-complete-practice')));
      await tester.pumpAndSettle();
      expect(find.text('结束面试练习？'), findsOneWidget);
      expect(find.text('结束后将保存本次回答并生成面试复盘。'), findsOneWidget);

      await tester.tap(find.byKey(const Key('interview-confirm-completion')));
      await tester.pumpAndSettle();
      expect(find.text('面试练习已完成'), findsOneWidget);
      expect(find.text('1 道回答已保存'), findsOneWidget);
      expect(
        find.byKey(const Key('interview-completion-drag-region')),
        findsOneWidget,
      );

      await tester.tap(find.byKey(const Key('interview-completion-primary')));
      await tester.pumpAndSettle();

      expect(await routeResult, CompletedPracticeRouteResult.returnToTraining);
      expect(find.text('Training root'), findsOneWidget);
    },
  );
}
