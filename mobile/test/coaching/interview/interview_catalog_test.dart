import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/features/coaching/interview/interview_catalog.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

void main() {
  testWidgets('separates plan content and preserves card actions', (
    tester,
  ) async {
    PracticePlanSummary? opened;
    PracticePlanSummary? deleted;
    final plan = _plan('plan-1');

    await _pumpCatalog(
      tester,
      plans: [plan],
      onPlanPressed: (value) => opened = value,
      onPlanDeleted: (value) => deleted = value,
    );

    expect(find.text('Java Developer'), findsOneWidget);
    expect(find.text('技术深挖'), findsOneWidget);
    expect(find.text('约 15 分钟'), findsOneWidget);
    expect(find.text('1–3 轮'), findsOneWidget);
    expect(find.text('模拟面试'), findsOneWidget);
    expect(
      find.byKey(const Key('interview-plan-cover-plan-1')),
      findsOneWidget,
    );
    expect(find.textContaining('技术深挖 ·'), findsNothing);

    await tester.tap(find.byKey(const Key('interview-plan-plan-1')));
    await tester.pump();
    expect(opened, same(plan));

    await tester.tap(find.byKey(const Key('delete-interview-plan-plan-1')));
    await tester.pumpAndSettle();
    expect(find.text('删除模拟面试？'), findsOneWidget);
    expect(deleted, isNull);

    await tester.tap(find.byKey(const Key('confirm-delete-interview-plan')));
    await tester.pumpAndSettle();
    expect(deleted, same(plan));
  });

  testWidgets('renders loading, error, empty, and multiple plan states', (
    tester,
  ) async {
    var retries = 0;

    await _pumpCatalog(tester, loading: true);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    await _pumpCatalog(
      tester,
      errorMessage: '方案加载失败',
      onRetry: () => retries++,
    );
    expect(find.text('方案加载失败'), findsOneWidget);
    await tester.tap(find.text('重试'));
    expect(retries, 1);

    await _pumpCatalog(tester);
    expect(find.byKey(const Key('interview-plan-empty')), findsOneWidget);

    await _pumpCatalog(tester, plans: [_plan('plan-1'), _plan('plan-2')]);
    expect(find.byType(Card), findsNWidgets(2));
  });

  testWidgets('labels a user-controlled plan as open-ended', (tester) async {
    await _pumpCatalog(
      tester,
      plans: [_plan('plan-open', maxEffectiveTurns: 0)],
    );

    expect(find.text('开放轮次'), findsOneWidget);
    expect(find.text('1–0 轮'), findsNothing);
  });

  testWidgets('keeps plan metadata usable at 320px and 3x text', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = 3;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);

    await _pumpCatalog(tester, plans: [_plan('plan-1')]);

    for (final label in ['Java Developer', '技术深挖', '约 15 分钟', '1–3 轮']) {
      final content = find.text(label);
      await tester.ensureVisible(content);
      await tester.pumpAndSettle();
      expect(content, findsOneWidget);
    }
    final deleteButton = find.byKey(const Key('delete-interview-plan-plan-1'));
    await tester.ensureVisible(deleteButton);
    await tester.pumpAndSettle();
    expect(deleteButton.hitTestable(), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}

Future<void> _pumpCatalog(
  WidgetTester tester, {
  List<PracticePlanSummary> plans = const [],
  bool loading = false,
  String? errorMessage,
  ValueChanged<PracticePlanSummary>? onPlanPressed,
  ValueChanged<PracticePlanSummary>? onPlanDeleted,
  VoidCallback? onRetry,
}) {
  return tester.pumpWidget(
    MaterialApp(
      theme: SpeakUpTheme.light,
      home: Scaffold(
        body: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: InterviewCatalog(
            plans: plans,
            loading: loading,
            errorMessage: errorMessage,
            onCreatePressed: () {},
            onPlanPressed: onPlanPressed ?? (_) {},
            onPlanDeleted: onPlanDeleted ?? (_) {},
            onRetry: onRetry ?? () {},
          ),
        ),
      ),
    ),
  );
}

PracticePlanSummary _plan(String id, {int maxEffectiveTurns = 3}) {
  return PracticePlanSummary(
    id: id,
    revision: 1,
    status: PracticePlanStatus.ready,
    experience: PracticeExperience.interview,
    sceneName: 'Technical interview',
    practiceScope: '技术深挖',
    jobTitle: 'Java Developer',
    practiceObjectives: const ['清楚说明技术取舍'],
    resumeUsed: true,
    suggestedDurationSeconds: 900,
    minEffectiveTurns: 1,
    maxEffectiveTurns: maxEffectiveTurns,
    createdAt: DateTime.utc(2026, 8, 7),
    updatedAt: DateTime.utc(2026, 8, 7),
  );
}
