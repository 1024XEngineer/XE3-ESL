// This file owns the optional PDF selected for one InterviewPreparation.

import 'package:file_picker/file_picker.dart';

final class InterviewResumeFile {
  const InterviewResumeFile({required this.name, required this.bytes});

  final String name;
  final List<int> bytes;
}

abstract interface class InterviewResumeFilePicker {
  Future<InterviewResumeFile?> pickPdf();
}

final class SystemInterviewResumeFilePicker
    implements InterviewResumeFilePicker {
  const SystemInterviewResumeFilePicker();

  @override
  Future<InterviewResumeFile?> pickPdf() async {
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
    return InterviewResumeFile(name: file.name, bytes: bytes);
  }
}
