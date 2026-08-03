// 本文件封装 PDF 文件选择与受保护阅读链接打开等平台能力。

import 'package:file_picker/file_picker.dart';
import 'package:url_launcher/url_launcher.dart';

import 'resume_models.dart';

/// ResumeFilePicker 抽象平台文件选择器，便于状态层测试。
abstract interface class ResumeFilePicker {
  Future<ResumePdfFile?> pickPdf();
}

/// SystemResumeFilePicker 使用系统文件面板读取单个 PDF。
final class SystemResumeFilePicker implements ResumeFilePicker {
  const SystemResumeFilePicker();

  @override
  Future<ResumePdfFile?> pickPdf() async {
    final result = await FilePicker.platform.pickFiles(
      type: FileType.custom,
      allowedExtensions: const <String>['pdf'],
      allowMultiple: false,
      withData: true,
    );
    final file = result?.files.single;
    final bytes = file?.bytes;
    if (file == null || bytes == null) {
      return null;
    }
    return ResumePdfFile(name: file.name, bytes: bytes);
  }
}

/// ResumeUrlOpener 抽象外部 PDF 查看能力。
abstract interface class ResumeUrlOpener {
  Future<bool> open(Uri url);
}

/// SystemResumeUrlOpener 使用系统安全浏览器打开短时 PDF 地址。
final class SystemResumeUrlOpener implements ResumeUrlOpener {
  const SystemResumeUrlOpener();

  @override
  Future<bool> open(Uri url) =>
      launchUrl(url, mode: LaunchMode.externalApplication);
}
