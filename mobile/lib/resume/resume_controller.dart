// 本文件编排简历列表、文件操作、详情编辑和用户可见状态。

import 'package:flutter/foundation.dart';

import 'resume_client.dart';
import 'resume_file_access.dart';
import 'resume_models.dart';

/// ResumeController 为“我的”页和简历管理页提供同一份可观察状态。
final class ResumeController extends ChangeNotifier {
  ResumeController({
    required this.client,
    required this.filePicker,
    required this.urlOpener,
  });

  static const maxResumes = 3;
  static const maxPdfBytes = 10 * 1024 * 1024;

  final ResumeClient client;
  final ResumeFilePicker filePicker;
  final ResumeUrlOpener urlOpener;

  List<ResumeItem> _items = const <ResumeItem>[];
  final Map<String, ResumeDetail> _details = <String, ResumeDetail>{};
  bool _loading = false;
  String? _busyResumeId;
  String? _errorMessage;
  String? _noticeMessage;
  ResumeItem? _temporaryItem;
  int _epoch = 0;

  List<ResumeItem> get items => List<ResumeItem>.unmodifiable(_items);
  bool get isLoading => _loading;
  bool get canUpload => _items.length < maxResumes && !_loading;
  String? get busyResumeId => _busyResumeId;
  String? get errorMessage => _errorMessage;
  String? get noticeMessage => _noticeMessage;
  ResumeItem? get temporaryItem => _temporaryItem;

  /// 返回已缓存的详情，未加载时返回空。
  ResumeDetail? detailFor(String resumeId) => _details[resumeId];

  /// 加载当前账号的最多三份简历。
  Future<void> load() async {
    if (_loading) return;
    final epoch = _epoch;
    _loading = true;
    _errorMessage = null;
    notifyListeners();
    try {
      final loaded = await client.list();
      if (epoch != _epoch) return;
      _items = loaded;
    } on Object catch (error) {
      if (epoch == _epoch) _errorMessage = _messageFor(error);
    } finally {
      if (epoch == _epoch) {
        _loading = false;
        notifyListeners();
      }
    }
  }

  /// 打开系统文件面板并上传新简历；用户取消选择时不产生错误。
  Future<void> pickAndUpload() async {
    if (!canUpload) {
      _setNotice('每个账号最多保存 3 份简历。');
      return;
    }
    final file = await filePicker.pickPdf();
    if (file == null) return;
    final validation = _validatePdf(file);
    if (validation != null) {
      _setNotice(validation);
      return;
    }
    await _runAction(null, () async {
      final title = file.name.replaceFirst(
        RegExp(r'\.pdf$', caseSensitive: false),
        '',
      );
      final created = await client.create(title: title, file: file);
      _items = <ResumeItem>[created, ..._items];
      _setNotice('简历已上传，正在解析。', notify: false);
    });
  }

  /// 上传仅供当前面试使用的简历，不进入已保存简历列表与额度。
  Future<void> pickTemporary() async {
    final file = await filePicker.pickPdf();
    if (file == null) return;
    final validation = _validatePdf(file);
    if (validation != null) {
      _setNotice(validation);
      return;
    }
    await _runAction(null, () async {
      final previous = _temporaryItem;
      final created = await client.createTemporary(file);
      _temporaryItem = created;
      if (previous != null) {
        await client.deleteTemporary(previous);
      }
      _setNotice('临时简历已上传，正在解析。', notify: false);
    });
  }

  Future<void> refreshTemporary() async {
    final current = _temporaryItem;
    if (current == null) return;
    await _runAction(current.id, () async {
      final detail = await client.getTemporary(current.id);
      _temporaryItem = detail.resume;
    });
  }

  Future<void> retryTemporaryParse() async {
    final current = _temporaryItem;
    if (current == null) return;
    await _runAction(current.id, () async {
      _temporaryItem = await client.retryTemporaryParse(current);
      _setNotice('已重新提交临时简历解析。', notify: false);
    });
  }

  Future<void> deleteTemporary() async {
    final current = _temporaryItem;
    if (current == null) return;
    await _runAction(current.id, () async {
      await client.deleteTemporary(current);
      _temporaryItem = null;
    });
  }

  /// 修改一份简历的展示名称。
  Future<void> rename(ResumeItem resume, String title) async {
    final value = title.trim();
    if (value.isEmpty || value.length > 120) {
      _setNotice('简历名称需为 1—120 个字符。');
      return;
    }
    await _runAction(resume.id, () async {
      final updated = await client.rename(resume, value);
      _replaceItem(updated);
      _setNotice('简历名称已更新。', notify: false);
    });
  }

  /// 选择新的 PDF 并替换指定简历文件。
  Future<void> pickAndReplace(ResumeItem resume) async {
    final file = await filePicker.pickPdf();
    if (file == null) return;
    final validation = _validatePdf(file);
    if (validation != null) {
      _setNotice(validation);
      return;
    }
    await _runAction(resume.id, () async {
      final updated = await client.replace(resume, file);
      _replaceItem(updated);
      _details.remove(resume.id);
      _setNotice('PDF 已替换，正在重新解析。', notify: false);
    });
  }

  /// 删除指定简历并同步移除本地详情缓存。
  Future<void> delete(ResumeItem resume) async {
    await _runAction(resume.id, () async {
      await client.delete(resume);
      _items = _items
          .where((item) => item.id != resume.id)
          .toList(growable: false);
      _details.remove(resume.id);
      _setNotice('简历已删除。', notify: false);
    });
  }

  /// 对解析失败的简历重新发起解析。
  Future<void> retryParse(ResumeItem resume) async {
    await _runAction(resume.id, () async {
      final updated = await client.retryParse(resume);
      _replaceItem(updated);
      _setNotice('已重新提交解析。', notify: false);
    });
  }

  /// 加载并缓存指定简历的结构化详情。
  Future<void> loadDetail(String resumeId) async {
    await _runAction(resumeId, () async {
      final detail = await client.get(resumeId);
      _details[resumeId] = detail;
      _replaceItem(detail.resume);
    });
  }

  /// 保存人工修改后的结构化内容修订。
  Future<void> saveContent(ResumeDetail detail, ResumeContent content) async {
    await _runAction(detail.resume.id, () async {
      final updated = await client.updateContent(detail, content);
      _details[detail.resume.id] = updated;
      _replaceItem(updated.resume);
      _setNotice('简历内容已保存。', notify: false);
    });
  }

  /// 获取短时地址并交给系统应用查看原始 PDF。
  Future<void> openPdf(String resumeId) async {
    await _runAction(resumeId, () async {
      final url = await client.getContentUrl(resumeId);
      if (!await urlOpener.open(url)) {
        throw const ResumeException(ResumeFailureKind.invalidResponse);
      }
    });
  }

  /// 清空上一个账号的简历数据，阻止迟到请求污染新账号。
  Future<void> clearPrivateState() async {
    _epoch++;
    _items = const <ResumeItem>[];
    _details.clear();
    _loading = false;
    _busyResumeId = null;
    _errorMessage = null;
    _noticeMessage = null;
    _temporaryItem = null;
    await client.clearAccountState();
    notifyListeners();
  }

  /// 消费一次性提示，避免 Widget 重建后重复展示。
  void consumeNotice() {
    if (_noticeMessage == null) return;
    _noticeMessage = null;
  }

  Future<void> _runAction(
    String? resumeId,
    Future<void> Function() action,
  ) async {
    if (_busyResumeId != null) return;
    final epoch = _epoch;
    _busyResumeId = resumeId ?? '__upload__';
    _errorMessage = null;
    notifyListeners();
    try {
      await action();
    } on Object catch (error) {
      if (epoch == _epoch) _setNotice(_messageFor(error), notify: false);
    } finally {
      if (epoch == _epoch) {
        _busyResumeId = null;
        notifyListeners();
      }
    }
  }

  void _replaceItem(ResumeItem updated) {
    _items = _items
        .map((item) => item.id == updated.id ? updated : item)
        .toList(growable: false);
  }

  String? _validatePdf(ResumePdfFile file) {
    if (!file.name.toLowerCase().endsWith('.pdf') ||
        file.bytes.length < 5 ||
        String.fromCharCodes(file.bytes.take(5)) != '%PDF-') {
      return '请选择有效的 PDF 文件。';
    }
    if (file.bytes.length > maxPdfBytes) return 'PDF 大小不能超过 10 MiB。';
    return null;
  }

  void _setNotice(String message, {bool notify = true}) {
    _noticeMessage = message;
    if (notify) notifyListeners();
  }
}

String _messageFor(Object error) {
  if (error is! ResumeException) return '简历操作暂时失败，请稍后重试。';
  return switch (error.kind) {
    ResumeFailureKind.authenticationRequired => '登录状态已失效，请重新登录。',
    ResumeFailureKind.invalidRequest => '提交的简历内容不符合要求。',
    ResumeFailureKind.notFound => '这份简历已不存在，请刷新列表。',
    ResumeFailureKind.conflict => '简历已在其他设备更新，请刷新后重试。',
    ResumeFailureKind.limitReached => '每个账号最多保存 3 份简历。',
    ResumeFailureKind.network => '网络连接异常，请稍后重试。',
    ResumeFailureKind.server => '简历服务暂时不可用，请稍后重试。',
    ResumeFailureKind.invalidResponse => '暂时无法读取简历，请稍后重试。',
    ResumeFailureKind.superseded => '账号已切换，本次操作已取消。',
  };
}
