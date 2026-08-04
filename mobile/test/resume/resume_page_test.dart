// 本文件验证简历概览入口、空状态和三份上限的关键 Widget 行为。

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/app/speak_up_app.dart';
import 'package:speakup/design/speak_up_theme.dart';
import 'package:speakup/resume/resume.dart';

void main() {
  testWidgets('the My tab contains the resume module entry', (tester) async {
    final controller = _controller(const <ResumeItem>[]);
    await tester.pumpWidget(SpeakUpApp.preview(resumeController: controller));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('primary-tab-profile')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('profile-page')), findsOneWidget);
    expect(find.byKey(const Key('profile-resume-card')), findsOneWidget);
    expect(find.text('我的简历'), findsOneWidget);
  });

  testWidgets('summary card opens the empty resume management page', (
    tester,
  ) async {
    final controller = _controller(const <ResumeItem>[]);
    await tester.pumpWidget(_app(ResumeSummaryCard(controller: controller)));
    await tester.pumpAndSettle();

    expect(find.text('我的简历'), findsOneWidget);
    expect(find.textContaining('上传 PDF'), findsOneWidget);

    await tester.tap(find.byKey(const Key('profile-resume-card')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('resume-page')), findsOneWidget);
    expect(find.text('从第一份简历开始'), findsOneWidget);
    expect(find.byKey(const Key('resume-empty-upload-button')), findsOneWidget);
  });

  testWidgets('three resumes show status and disable upload', (tester) async {
    final controller = _controller(<ResumeItem>[
      _resume('1', ResumeParseStatus.ready),
      _resume('2', ResumeParseStatus.parsing),
      _resume('3', ResumeParseStatus.failed),
    ]);
    await tester.pumpWidget(_app(ResumePage(controller: controller)));
    await tester.pumpAndSettle();

    expect(find.text('3 / 3'), findsOneWidget);
    expect(find.text('已解析'), findsOneWidget);
    expect(find.text('解析中'), findsOneWidget);
    expect(find.text('解析失败'), findsOneWidget);
    final button = tester.widget<FilledButton>(
      find.byKey(const Key('resume-upload-button')),
    );
    expect(button.onPressed, isNull);
    expect(find.text('已达到 3 份上限'), findsOneWidget);
  });

  testWidgets('failed detail explains how to replace an image-only PDF', (
    tester,
  ) async {
    final failed = _resume('failed', ResumeParseStatus.failed);
    final controller = _controller(<ResumeItem>[failed]);
    await tester.pumpWidget(
      _app(ResumeDetailPage(controller: controller, resumeId: failed.id)),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('图片型 PDF'), findsOneWidget);
    expect(find.textContaining('带可选中文本'), findsOneWidget);
  });

  testWidgets('project summary opens the complete parsed project details', (
    tester,
  ) async {
    final ready = _resume('ready', ResumeParseStatus.ready);
    final controller = _controller(
      <ResumeItem>[ready],
      content: const ResumeContent(
        projectExperiences: <Map<String, Object?>>[
          <String, Object?>{
            'project_name': '杭电 Ragent 工程平台',
            'role': '后端开发',
            'description': '面向校园知识库的企业级 RAG 系统。',
            'technologies': <Object?>['SpringBoot', 'Milvus'],
            'duties': <Object?>['设计混合检索与重排序链路。'],
            'achievements': <Object?>['Top-5 命中率提升至 91%。'],
          },
        ],
      ),
    );
    await tester.pumpWidget(
      _app(ResumeDetailPage(controller: controller, resumeId: ready.id)),
    );
    await tester.pumpAndSettle();

    final projectCard = find.byKey(const Key('resume-content-projects'));
    await tester.ensureVisible(projectCard);
    await tester.drag(find.byType(ListView), const Offset(0, -120));
    await tester.pumpAndSettle();
    await tester.tap(projectCard);
    await tester.pumpAndSettle();

    expect(find.text('项目介绍'), findsOneWidget);
    expect(find.text('面向校园知识库的企业级 RAG 系统。'), findsOneWidget);
    expect(find.text('SpringBoot'), findsOneWidget);
    expect(find.text('设计混合检索与重排序链路。'), findsOneWidget);
    expect(find.text('Top-5 命中率提升至 91%。'), findsOneWidget);
  });

  testWidgets('summary is hidden and awards open as a complete list', (
    tester,
  ) async {
    final ready = _resume('awards', ResumeParseStatus.ready);
    final controller = _controller(
      <ResumeItem>[ready],
      content: const ResumeContent(
        professionalSummary: '不应展示的兼容字段',
        awards: <String>['浙江省政府奖学金', '数学建模国赛省三等奖'],
      ),
    );
    await tester.pumpWidget(
      _app(ResumeDetailPage(controller: controller, resumeId: ready.id)),
    );
    await tester.pumpAndSettle();

    expect(find.text('个人简介'), findsNothing);
    expect(find.text('不应展示的兼容字段'), findsNothing);
    final awardsCard = find.byKey(const Key('resume-content-awards'));
    await tester.ensureVisible(awardsCard);
    await tester.tap(awardsCard);
    await tester.pumpAndSettle();

    expect(find.text('浙江省政府奖学金'), findsWidgets);
    expect(find.text('数学建模国赛省三等奖'), findsWidgets);
  });

  testWidgets('education detail displays the original GPA scale', (
    tester,
  ) async {
    final ready = _resume('education', ResumeParseStatus.ready);
    final controller = _controller(
      <ResumeItem>[ready],
      content: const ResumeContent(
        educationExperiences: <Map<String, Object?>>[
          <String, Object?>{
            'school': '杭州电子科技大学',
            'major': '计算机科学与技术',
            'degree': '本科',
            'gpa': '4.588/5.0',
          },
        ],
      ),
    );
    await tester.pumpWidget(
      _app(ResumeDetailPage(controller: controller, resumeId: ready.id)),
    );
    await tester.pumpAndSettle();

    final educationCard = find.byKey(const Key('resume-content-education'));
    await tester.ensureVisible(educationCard);
    await tester.drag(find.byType(ListView), const Offset(0, -100));
    await tester.pumpAndSettle();
    await tester.tap(educationCard);
    await tester.pumpAndSettle();

    expect(find.text('绩点'), findsOneWidget);
    expect(find.text('4.588/5.0'), findsOneWidget);
  });
}

Widget _app(Widget home) => MaterialApp(theme: SpeakUpTheme.light, home: home);

ResumeController _controller(
  List<ResumeItem> items, {
  ResumeContent? content,
}) => ResumeController(
  client: _PageClient(items, content),
  filePicker: _NoopPicker(),
  urlOpener: _NoopOpener(),
);

ResumeItem _resume(String id, ResumeParseStatus status) => ResumeItem(
  id: id,
  title: '简历 $id',
  originalFilename: '$id.pdf',
  sizeBytes: 1024,
  parseStatus: status,
  version: 1,
  updatedAt: DateTime.utc(2026, 8, 3),
);

final class _NoopPicker implements ResumeFilePicker {
  @override
  Future<ResumePdfFile?> pickPdf() async => null;
}

final class _NoopOpener implements ResumeUrlOpener {
  @override
  Future<bool> open(Uri url) async => true;
}

final class _PageClient implements ResumeClient {
  _PageClient(this.items, this.content);
  final List<ResumeItem> items;
  final ResumeContent? content;
  @override
  Future<List<ResumeItem>> list() async => items;
  @override
  Future<void> clearAccountState() async {}
  @override
  Future<ResumeItem> create({
    required String title,
    required ResumePdfFile file,
  }) => throw UnimplementedError();
  @override
  Future<void> delete(ResumeItem resume) => throw UnimplementedError();
  @override
  Future<ResumeDetail> get(String resumeId) async => ResumeDetail(
    resume: items.singleWhere((item) => item.id == resumeId),
    content: content,
  );
  @override
  Future<Uri> getContentUrl(String resumeId) => throw UnimplementedError();
  @override
  Future<ResumeItem> rename(ResumeItem resume, String title) =>
      throw UnimplementedError();
  @override
  Future<ResumeItem> replace(ResumeItem resume, ResumePdfFile file) =>
      throw UnimplementedError();
  @override
  Future<ResumeItem> retryParse(ResumeItem resume) =>
      throw UnimplementedError();
  @override
  Future<ResumeDetail> updateContent(
    ResumeDetail detail,
    ResumeContent content,
  ) => throw UnimplementedError();
}
