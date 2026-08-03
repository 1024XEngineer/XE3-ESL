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
}

Widget _app(Widget home) => MaterialApp(theme: SpeakUpTheme.light, home: home);

ResumeController _controller(List<ResumeItem> items) => ResumeController(
  client: _PageClient(items),
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
  _PageClient(this.items);
  final List<ResumeItem> items;
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
  Future<ResumeDetail> get(String resumeId) async =>
      ResumeDetail(resume: items.singleWhere((item) => item.id == resumeId));
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
