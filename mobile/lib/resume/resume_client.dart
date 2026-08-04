// 本文件定义 Resume 应用层依赖的远端接口和统一失败类型。

import 'resume_models.dart';

/// ResumeClient 描述简历页面需要的全部后端能力。
abstract interface class ResumeClient {
  Future<List<ResumeItem>> list();
  Future<ResumeItem> create({
    required String title,
    required ResumePdfFile file,
  });
  Future<ResumeDetail> get(String resumeId);
  Future<ResumeItem> rename(ResumeItem resume, String title);
  Future<ResumeItem> replace(ResumeItem resume, ResumePdfFile file);
  Future<void> delete(ResumeItem resume);
  Future<ResumeItem> retryParse(ResumeItem resume);
  Future<ResumeDetail> updateContent(
    ResumeDetail detail,
    ResumeContent content,
  );
  Future<Uri> getContentUrl(String resumeId);
  Future<void> clearAccountState();
}

enum ResumeFailureKind {
  authenticationRequired,
  invalidRequest,
  notFound,
  conflict,
  limitReached,
  network,
  invalidResponse,
  server,
  superseded,
}

/// ResumeException 保存可安全呈现给状态层的失败分类。
final class ResumeException implements Exception {
  const ResumeException(this.kind, {this.retryable = false});

  final ResumeFailureKind kind;
  final bool retryable;

  @override
  String toString() => 'ResumeException(${kind.name})';
}
