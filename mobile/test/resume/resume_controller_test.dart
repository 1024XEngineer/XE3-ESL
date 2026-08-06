// 本文件验证简历状态编排、三份上限和 PDF 本地校验。

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/resume/resume.dart';

void main() {
  test('loads resumes and blocks a fourth upload', () async {
    final client = _FakeResumeClient(
      items: <ResumeItem>[_resume('1'), _resume('2'), _resume('3')],
    );
    final picker = _FakePicker(
      ResumePdfFile(name: 'fourth.pdf', bytes: '%PDF-content'.codeUnits),
    );
    final controller = ResumeController(
      client: client,
      filePicker: picker,
      urlOpener: _FakeOpener(),
    );

    await controller.load();
    await controller.pickAndUpload();

    expect(controller.items, hasLength(3));
    expect(controller.canUpload, isFalse);
    expect(controller.noticeMessage, '每个账号最多保存 3 份简历。');
    expect(picker.pickCount, 0);
    expect(client.createCount, 0);
  });

  test('valid PDF upload uses filename as title and joins the list', () async {
    final client = _FakeResumeClient();
    final picker = _FakePicker(
      ResumePdfFile(
        name: 'Backend Engineer.pdf',
        bytes: '%PDF-content'.codeUnits,
      ),
    );
    final controller = ResumeController(
      client: client,
      filePicker: picker,
      urlOpener: _FakeOpener(),
    );

    await controller.load();
    await controller.pickAndUpload();

    expect(client.createdTitle, 'Backend Engineer');
    expect(controller.items.single.title, 'Backend Engineer');
    expect(controller.noticeMessage, '简历已上传，正在解析。');
  });

  test('rejects a renamed non-PDF before network upload', () async {
    final client = _FakeResumeClient();
    final controller = ResumeController(
      client: client,
      filePicker: _FakePicker(
        ResumePdfFile(name: 'fake.pdf', bytes: 'not-a-pdf'.codeUnits),
      ),
      urlOpener: _FakeOpener(),
    );

    await controller.pickAndUpload();

    expect(client.createCount, 0);
    expect(controller.noticeMessage, '请选择有效的 PDF 文件。');
  });

  test('temporary upload stays outside the saved resume list', () async {
    final client = _FakeResumeClient(items: <ResumeItem>[_resume('saved')]);
    final controller = ResumeController(
      client: client,
      filePicker: _FakePicker(
        ResumePdfFile(name: 'temporary.pdf', bytes: '%PDF-temp'.codeUnits),
      ),
      urlOpener: _FakeOpener(),
    );

    await controller.load();
    await controller.pickTemporary();

    expect(controller.items.single.id, 'saved');
    expect(controller.temporaryItem?.id, 'temporary');
    expect(controller.canUpload, isTrue);
  });
}

ResumeItem _resume(String id, {String? title}) => ResumeItem(
  id: id,
  title: title ?? '简历 $id',
  originalFilename: '$id.pdf',
  sizeBytes: 2048,
  parseStatus: ResumeParseStatus.ready,
  currentRevision: 1,
  version: 1,
  updatedAt: DateTime.utc(2026, 8, 3),
);

final class _FakePicker implements ResumeFilePicker {
  _FakePicker(this.file);
  final ResumePdfFile? file;
  int pickCount = 0;
  @override
  Future<ResumePdfFile?> pickPdf() async {
    pickCount++;
    return file;
  }
}

final class _FakeOpener implements ResumeUrlOpener {
  @override
  Future<bool> open(Uri url) async => true;
}

final class _FakeResumeClient implements ResumeClient {
  _FakeResumeClient({this.items = const <ResumeItem>[]});
  List<ResumeItem> items;
  int createCount = 0;
  String? createdTitle;

  @override
  Future<List<ResumeItem>> list() async => items;

  @override
  Future<ResumeItem> create({
    required String title,
    required ResumePdfFile file,
  }) async {
    createCount++;
    createdTitle = title;
    return _resume('created', title: title);
  }

  @override
  Future<ResumeItem> createTemporary(ResumePdfFile file) async =>
      _resume('temporary');
  @override
  Future<void> deleteTemporary(ResumeItem resume) async {}
  @override
  Future<ResumeDetail> getTemporary(String resumeId) async =>
      ResumeDetail(resume: _resume(resumeId));
  @override
  Future<ResumeItem> retryTemporaryParse(ResumeItem resume) async => resume;

  @override
  Future<void> clearAccountState() async {}
  @override
  Future<void> delete(ResumeItem resume) async {}
  @override
  Future<ResumeDetail> get(String resumeId) async =>
      ResumeDetail(resume: _resume(resumeId));
  @override
  Future<Uri> getContentUrl(String resumeId) async =>
      Uri.parse('https://example.test/resume.pdf');
  @override
  Future<ResumeItem> rename(ResumeItem resume, String title) async =>
      _resume(resume.id, title: title);
  @override
  Future<ResumeItem> replace(ResumeItem resume, ResumePdfFile file) async =>
      resume;
  @override
  Future<ResumeItem> retryParse(ResumeItem resume) async => resume;
  @override
  Future<ResumeDetail> updateContent(
    ResumeDetail detail,
    ResumeContent content,
  ) async => ResumeDetail(resume: detail.resume, content: content);
}
