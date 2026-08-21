import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/design/speak_up_theme.dart';

void main() {
  test('theme exposes the shared semantic visual tokens', () {
    final theme = SpeakUpTheme.light;

    expect(theme.scaffoldBackgroundColor, SpeakUpDesign.canvas);
    expect(theme.colorScheme.primary, SpeakUpDesign.primary);
    expect(SpeakUpDesign.primary, SpeakUpDesign.ink);
    expect(theme.progressIndicatorTheme.color, SpeakUpDesign.primary);
    expect(theme.cardTheme.color, SpeakUpDesign.surface);
    expect(
      theme.textTheme.headlineLarge?.fontSize,
      SpeakUpDesign.pageTitle.fontSize,
    );
    expect(
      theme.textTheme.titleLarge?.fontWeight,
      SpeakUpDesign.sectionTitle.fontWeight,
    );
    expect(
      theme.textTheme.titleMedium?.fontSize,
      SpeakUpDesign.cardTitle.fontSize,
    );
    expect(theme.inputDecorationTheme.fillColor, SpeakUpDesign.surface);
    expect(theme.bottomSheetTheme.showDragHandle, isTrue);
  });

  testWidgets(
    'foundation components remain usable on narrow large-text screens',
    (tester) async {
      tester.view.physicalSize = const Size(320, 568);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      await tester.pumpWidget(
        MediaQuery(
          data: const MediaQueryData(
            size: Size(320, 568),
            textScaler: TextScaler.linear(2),
          ),
          child: MaterialApp(
            theme: SpeakUpTheme.light,
            home: Scaffold(
              body: SpeakUpPage(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    const SpeakUpDisplayTitle(
                      title: 'Practice',
                      semanticLabel: '训练',
                    ),
                    const SizedBox(height: SpeakUpDesign.space24),
                    const SpeakUpPageHeader(
                      title: '英文表达训练',
                      subtitle: '一次只完成一个清晰任务。',
                      leading: SpeakUpBackButton(),
                    ),
                    const SizedBox(height: SpeakUpDesign.space24),
                    const SpeakUpSectionHeader(
                      title: '继续练习',
                      subtitle: '回到上次停下的位置',
                    ),
                    const SizedBox(height: SpeakUpDesign.space12),
                    SpeakUpTaskCard(
                      title: '项目经历深挖',
                      subtitle: '围绕真实经历完成一轮面试表达',
                      semanticLabel: '继续项目经历深挖',
                      onTap: () {},
                      trailing: const Icon(Icons.chevron_right_rounded),
                    ),
                    const SizedBox(height: SpeakUpDesign.space16),
                    SpeakUpStepRow(
                      index: 1,
                      title: '准备背景',
                      subtitle: '补充岗位与项目情况',
                      selected: true,
                      onTap: () {},
                    ),
                    const SpeakUpEmptyState(
                      title: '暂无历史',
                      message: '完成一次练习后，复盘会出现在这里。',
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      );

      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.text('Practice'), findsOneWidget);
      expect(find.text('英文表达训练'), findsOneWidget);
      expect(find.text('项目经历深挖'), findsOneWidget);

      expect(
        tester
            .widget<Semantics>(
              find
                  .descendant(
                    of: find.byType(SpeakUpTaskCard),
                    matching: find.byType(Semantics),
                  )
                  .first,
            )
            .properties
            .label,
        '继续项目经历深挖',
      );
    },
  );

  testWidgets('display title exposes the localized page heading', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: SpeakUpDisplayTitle(title: 'Review', semanticLabel: '复盘'),
        ),
      ),
    );

    final semantics = tester.ensureSemantics();
    expect(find.bySemanticsLabel('复盘'), findsOneWidget);
    expect(
      tester.widget<Text>(find.text('Review')).style?.fontFamily,
      SpeakUpDesign.displayTitle.fontFamily,
    );
    semantics.dispose();
  });

  testWidgets('interactive controls meet the 44 point minimum target', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: SpeakUpTheme.light,
        home: Scaffold(
          body: Column(
            children: [
              const SpeakUpBackButton(),
              FilledButton(onPressed: () {}, child: const Text('开始')),
              OutlinedButton(onPressed: () {}, child: const Text('稍后')),
              TextButton(onPressed: () {}, child: const Text('查看')),
            ],
          ),
        ),
      ),
    );

    expect(
      tester.getSize(find.byType(IconButton)).height,
      greaterThanOrEqualTo(44),
    );
    expect(
      tester.getSize(find.byType(FilledButton)).height,
      greaterThanOrEqualTo(44),
    );
    expect(
      tester.getSize(find.byType(OutlinedButton)).height,
      greaterThanOrEqualTo(44),
    );
    expect(
      tester.getSize(find.byType(TextButton)).height,
      greaterThanOrEqualTo(44),
    );
  });
}
