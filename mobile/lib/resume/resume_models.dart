// 本文件定义移动端 Resume 模块使用的领域模型与 JSON 映射。

enum ResumeParseStatus { queued, parsing, ready, failed }

/// 描述一份简历的列表元数据和当前并发版本。
final class ResumeItem {
  const ResumeItem({
    required this.id,
    required this.title,
    required this.originalFilename,
    required this.sizeBytes,
    required this.parseStatus,
    required this.version,
    required this.updatedAt,
  });

  final String id;
  final String title;
  final String originalFilename;
  final int sizeBytes;
  final ResumeParseStatus parseStatus;
  final int version;
  final DateTime updatedAt;

  /// 从服务端契约对象创建简历元数据。
  factory ResumeItem.fromJson(Map<String, Object?> json) {
    return ResumeItem(
      id: _requiredString(json, 'resume_id'),
      title: _requiredString(json, 'title'),
      originalFilename: _requiredString(json, 'original_filename'),
      sizeBytes: _requiredInt(json, 'size_bytes'),
      parseStatus: switch (_requiredString(json, 'parse_status')) {
        'QUEUED' => ResumeParseStatus.queued,
        'PARSING' => ResumeParseStatus.parsing,
        'READY' => ResumeParseStatus.ready,
        'FAILED' => ResumeParseStatus.failed,
        _ => throw const FormatException('Invalid resume parse status.'),
      },
      version: _requiredInt(json, 'version'),
      updatedAt: DateTime.parse(_requiredString(json, 'updated_at')),
    );
  }
}

/// 保存解析后或人工修订的结构化简历内容。
final class ResumeContent {
  const ResumeContent({
    this.targetPosition = '',
    this.professionalSummary = '',
    this.workExperiences = const <Map<String, Object?>>[],
    this.projectExperiences = const <Map<String, Object?>>[],
    this.educationExperiences = const <Map<String, Object?>>[],
    this.skills = const <String>[],
  });

  final String targetPosition;
  final String professionalSummary;
  final List<Map<String, Object?>> workExperiences;
  final List<Map<String, Object?>> projectExperiences;
  final List<Map<String, Object?>> educationExperiences;
  final List<String> skills;

  /// 从服务端结构化内容创建不可变模型。
  factory ResumeContent.fromJson(Map<String, Object?> json) {
    return ResumeContent(
      targetPosition: _requiredString(
        json,
        'target_position',
        allowEmpty: true,
      ),
      professionalSummary: _requiredString(
        json,
        'professional_summary',
        allowEmpty: true,
      ),
      workExperiences: _objectList(json, 'work_experiences'),
      projectExperiences: _objectList(json, 'project_experiences'),
      educationExperiences: _objectList(json, 'education_experiences'),
      skills: _stringList(json, 'skills'),
    );
  }

  /// 生成更新结构化内容接口需要的 JSON 对象。
  Map<String, Object?> toJson() => <String, Object?>{
    'target_position': targetPosition,
    'professional_summary': professionalSummary,
    'work_experiences': workExperiences,
    'project_experiences': projectExperiences,
    'education_experiences': educationExperiences,
    'skills': skills,
  };

  /// 复制内容并替换当前界面允许人工编辑的字段。
  ResumeContent copyWith({
    String? targetPosition,
    String? professionalSummary,
    List<String>? skills,
  }) {
    return ResumeContent(
      targetPosition: targetPosition ?? this.targetPosition,
      professionalSummary: professionalSummary ?? this.professionalSummary,
      workExperiences: workExperiences,
      projectExperiences: projectExperiences,
      educationExperiences: educationExperiences,
      skills: skills ?? this.skills,
    );
  }
}

/// 表示简历详情及其当前结构化修订。
final class ResumeDetail {
  const ResumeDetail({required this.resume, this.content});

  final ResumeItem resume;
  final ResumeContent? content;

  /// 从详情接口响应创建模型。
  factory ResumeDetail.fromJson(Map<String, Object?> json) {
    final resume = _requiredObject(json, 'resume');
    final revision = json['current_revision'];
    return ResumeDetail(
      resume: ResumeItem.fromJson(resume),
      content: revision is Map<String, Object?>
          ? ResumeContent.fromJson(_requiredObject(revision, 'content'))
          : null,
    );
  }
}

/// 表示由系统文件选择器返回的 PDF 文件。
final class ResumePdfFile {
  const ResumePdfFile({required this.name, required this.bytes});

  final String name;
  final List<int> bytes;
}

String _requiredString(
  Map<String, Object?> json,
  String key, {
  bool allowEmpty = false,
}) {
  final value = json[key];
  if (value is! String || (!allowEmpty && value.isEmpty)) {
    throw FormatException('Invalid $key.');
  }
  return value;
}

int _requiredInt(Map<String, Object?> json, String key) {
  final value = json[key];
  if (value is! int) {
    throw FormatException('Invalid $key.');
  }
  return value;
}

Map<String, Object?> _requiredObject(Map<String, Object?> json, String key) {
  final value = json[key];
  if (value is! Map<String, Object?>) {
    throw FormatException('Invalid $key.');
  }
  return value;
}

List<Map<String, Object?>> _objectList(Map<String, Object?> json, String key) {
  final value = json[key];
  if (value is! List<Object?>) {
    throw FormatException('Invalid $key.');
  }
  return List<Map<String, Object?>>.unmodifiable(
    value.map((item) {
      if (item is! Map<String, Object?>) {
        throw FormatException('Invalid $key item.');
      }
      return Map<String, Object?>.unmodifiable(item);
    }),
  );
}

List<String> _stringList(Map<String, Object?> json, String key) {
  final value = json[key];
  if (value is! List<Object?> || value.any((item) => item is! String)) {
    throw FormatException('Invalid $key.');
  }
  return List<String>.unmodifiable(value.cast<String>());
}
