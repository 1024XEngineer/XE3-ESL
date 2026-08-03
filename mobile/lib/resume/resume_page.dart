// 本文件提供“我的”页简历概览、简历管理页和结构化详情交互。

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';

import 'resume_controller.dart';
import 'resume_models.dart';

/// ResumeSummaryCard 在“我的”页面展示简历数量和解析概况。
final class ResumeSummaryCard extends StatefulWidget {
  const ResumeSummaryCard({required this.controller, super.key});

  final ResumeController controller;

  @override
  State<ResumeSummaryCard> createState() => _ResumeSummaryCardState();
}

class _ResumeSummaryCardState extends State<ResumeSummaryCard> {
  @override
  void initState() {
    super.initState();
    if (widget.controller.items.isEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(widget.controller.load());
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: widget.controller,
      builder: (context, _) {
        final items = widget.controller.items;
        final ready = items
            .where((item) => item.parseStatus == ResumeParseStatus.ready)
            .length;
        return Card(
          key: const Key('profile-resume-card'),
          child: InkWell(
            borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
            onTap: () => Navigator.of(context).push(
              MaterialPageRoute<void>(
                builder: (_) => ResumePage(controller: widget.controller),
              ),
            ),
            child: Padding(
              padding: const EdgeInsets.all(SpeakUpDesign.space16),
              child: Row(
                children: [
                  const _ResumeIcon(),
                  const SizedBox(width: SpeakUpDesign.space16),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '我的简历',
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        const SizedBox(height: SpeakUpDesign.space4),
                        Text(
                          widget.controller.isLoading && items.isEmpty
                              ? '正在读取简历…'
                              : items.isEmpty
                              ? '上传 PDF，让面试准备更贴合你的经历'
                              : '${items.length}/3 份 · $ready 份已解析',
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: SpeakUpDesign.space8),
                  const Icon(
                    Icons.chevron_right_rounded,
                    color: SpeakUpDesign.secondary,
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}

/// ResumePage 管理当前账号的最多三份 PDF 简历。
final class ResumePage extends StatefulWidget {
  const ResumePage({required this.controller, super.key});

  final ResumeController controller;

  @override
  State<ResumePage> createState() => _ResumePageState();
}

class _ResumePageState extends State<ResumePage> {
  Timer? _parsePollTimer;

  @override
  void initState() {
    super.initState();
    widget.controller.addListener(_showNotice);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(widget.controller.load());
    });
    _parsePollTimer = Timer.periodic(const Duration(seconds: 4), (_) {
      final hasPending = widget.controller.items.any(
        (item) =>
            item.parseStatus == ResumeParseStatus.queued ||
            item.parseStatus == ResumeParseStatus.parsing,
      );
      if (mounted &&
          hasPending &&
          !widget.controller.isLoading &&
          widget.controller.busyResumeId == null) {
        unawaited(widget.controller.load());
      }
    });
  }

  @override
  void dispose() {
    _parsePollTimer?.cancel();
    widget.controller.removeListener(_showNotice);
    super.dispose();
  }

  void _showNotice() {
    final message = widget.controller.noticeMessage;
    if (message == null || !mounted) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(message)));
      widget.controller.consumeNotice();
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('resume-page'),
      appBar: AppBar(leading: const SpeakUpBackButton()),
      body: SafeArea(
        child: AnimatedBuilder(
          animation: widget.controller,
          builder: (context, _) => RefreshIndicator(
            onRefresh: widget.controller.load,
            child: ListView(
              physics: const AlwaysScrollableScrollPhysics(),
              padding: SpeakUpDesign.pagePadding(context),
              children: [
                SpeakUpPageHeader(
                  title: '我的简历',
                  subtitle: '最多保存 3 份 PDF，解析后可检查并完善关键信息。',
                  trailing: _CountBadge(count: widget.controller.items.length),
                ),
                const SizedBox(height: SpeakUpDesign.space24),
                if (widget.controller.errorMessage != null) ...[
                  _ErrorCard(
                    message: widget.controller.errorMessage!,
                    onRetry: widget.controller.load,
                  ),
                  const SizedBox(height: SpeakUpDesign.space16),
                ],
                if (widget.controller.isLoading &&
                    widget.controller.items.isEmpty)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 56),
                    child: Center(child: CircularProgressIndicator()),
                  )
                else if (widget.controller.items.isEmpty)
                  _EmptyResumeCard(onUpload: widget.controller.pickAndUpload)
                else
                  for (final resume in widget.controller.items) ...[
                    _ResumeCard(
                      resume: resume,
                      busy: widget.controller.busyResumeId == resume.id,
                      onOpen: () => _openDetail(resume),
                      onRename: () => _rename(resume),
                      onReplace: () => widget.controller.pickAndReplace(resume),
                      onRetry: resume.parseStatus == ResumeParseStatus.failed
                          ? () => widget.controller.retryParse(resume)
                          : null,
                      onDelete: () => _delete(resume),
                    ),
                    const SizedBox(height: SpeakUpDesign.space12),
                  ],
                if (widget.controller.items.isNotEmpty) ...[
                  const SizedBox(height: SpeakUpDesign.space8),
                  FilledButton.icon(
                    key: const Key('resume-upload-button'),
                    onPressed:
                        widget.controller.canUpload &&
                            widget.controller.busyResumeId == null
                        ? widget.controller.pickAndUpload
                        : null,
                    icon: const Icon(Icons.upload_file_rounded),
                    label: Text(
                      widget.controller.items.length >=
                              ResumeController.maxResumes
                          ? '已达到 3 份上限'
                          : '上传 PDF 简历',
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _openDetail(ResumeItem resume) async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => ResumeDetailPage(
          controller: widget.controller,
          resumeId: resume.id,
        ),
      ),
    );
  }

  Future<void> _rename(ResumeItem resume) async {
    final controller = TextEditingController(text: resume.title);
    final title = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('重命名简历'),
        content: TextField(
          key: const Key('resume-title-input'),
          controller: controller,
          autofocus: true,
          maxLength: 120,
          decoration: const InputDecoration(labelText: '简历名称'),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('取消'),
          ),
          FilledButton(
            key: const Key('resume-title-save'),
            onPressed: () => Navigator.pop(context, controller.text),
            child: const Text('保存'),
          ),
        ],
      ),
    );
    controller.dispose();
    if (title != null) await widget.controller.rename(resume, title);
  }

  Future<void> _delete(ResumeItem resume) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除这份简历？'),
        content: Text('“${resume.title}”及其解析内容将被删除，此操作无法撤销。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            key: const Key('resume-delete-confirm'),
            style: FilledButton.styleFrom(backgroundColor: SpeakUpDesign.error),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed == true) await widget.controller.delete(resume);
  }
}

/// ResumeDetailPage 展示原始 PDF 入口及当前结构化简历内容。
final class ResumeDetailPage extends StatefulWidget {
  const ResumeDetailPage({
    required this.controller,
    required this.resumeId,
    super.key,
  });

  final ResumeController controller;
  final String resumeId;

  @override
  State<ResumeDetailPage> createState() => _ResumeDetailPageState();
}

class _ResumeDetailPageState extends State<ResumeDetailPage> {
  @override
  void initState() {
    super.initState();
    widget.controller.addListener(_refresh);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(widget.controller.loadDetail(widget.resumeId));
    });
  }

  @override
  void dispose() {
    widget.controller.removeListener(_refresh);
    super.dispose();
  }

  void _refresh() {
    if (mounted) setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    final detail = widget.controller.detailFor(widget.resumeId);
    final busy = widget.controller.busyResumeId == widget.resumeId;
    return Scaffold(
      key: const Key('resume-detail-page'),
      appBar: AppBar(leading: const SpeakUpBackButton()),
      body: SafeArea(
        child: detail == null
            ? Center(
                child: busy
                    ? const CircularProgressIndicator()
                    : FilledButton(
                        onPressed: () =>
                            widget.controller.loadDetail(widget.resumeId),
                        child: const Text('重新加载'),
                      ),
              )
            : ListView(
                padding: SpeakUpDesign.pagePadding(context),
                children: [
                  SpeakUpPageHeader(
                    title: detail.resume.title,
                    subtitle:
                        '${detail.resume.originalFilename} · ${_formatBytes(detail.resume.sizeBytes)}',
                  ),
                  const SizedBox(height: SpeakUpDesign.space20),
                  OutlinedButton.icon(
                    key: const Key('resume-open-pdf'),
                    onPressed: busy
                        ? null
                        : () => widget.controller.openPdf(widget.resumeId),
                    icon: const Icon(Icons.picture_as_pdf_rounded),
                    label: const Text('查看原始 PDF'),
                  ),
                  const SizedBox(height: SpeakUpDesign.space24),
                  SpeakUpSectionHeader(
                    title: '结构化内容',
                    subtitle: detail.content == null
                        ? '解析完成后会显示在这里'
                        : '可人工修正岗位、简介和技能',
                    action: detail.content == null
                        ? null
                        : TextButton.icon(
                            key: const Key('resume-edit-content'),
                            onPressed: busy ? null : () => _editContent(detail),
                            icon: const Icon(Icons.edit_rounded, size: 18),
                            label: const Text('编辑'),
                          ),
                  ),
                  const SizedBox(height: SpeakUpDesign.space12),
                  if (detail.content == null)
                    _ParsingCard(status: detail.resume.parseStatus)
                  else
                    _ResumeContentView(content: detail.content!),
                ],
              ),
      ),
    );
  }

  Future<void> _editContent(ResumeDetail detail) async {
    final content = detail.content!;
    final position = TextEditingController(text: content.targetPosition);
    final summary = TextEditingController(text: content.professionalSummary);
    final skills = TextEditingController(text: content.skills.join('、'));
    final updated = await showModalBottomSheet<ResumeContent>(
      context: context,
      isScrollControlled: true,
      builder: (context) => Padding(
        padding: EdgeInsets.fromLTRB(
          SpeakUpDesign.space20,
          SpeakUpDesign.space8,
          SpeakUpDesign.space20,
          MediaQuery.viewInsetsOf(context).bottom + SpeakUpDesign.space24,
        ),
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('完善简历内容', style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: SpeakUpDesign.space16),
              TextField(
                controller: position,
                maxLength: 200,
                decoration: const InputDecoration(labelText: '目标岗位'),
              ),
              const SizedBox(height: SpeakUpDesign.space12),
              TextField(
                controller: summary,
                maxLength: 4000,
                minLines: 4,
                maxLines: 8,
                decoration: const InputDecoration(labelText: '个人简介'),
              ),
              const SizedBox(height: SpeakUpDesign.space12),
              TextField(
                controller: skills,
                decoration: const InputDecoration(
                  labelText: '技能',
                  hintText: '使用逗号、顿号或换行分隔',
                ),
              ),
              const SizedBox(height: SpeakUpDesign.space16),
              FilledButton(
                key: const Key('resume-content-save'),
                onPressed: () => Navigator.pop(
                  context,
                  content.copyWith(
                    targetPosition: position.text.trim(),
                    professionalSummary: summary.text.trim(),
                    skills: skills.text
                        .split(RegExp(r'[,，、\n]'))
                        .map((value) => value.trim())
                        .where((value) => value.isNotEmpty)
                        .toSet()
                        .take(100)
                        .toList(growable: false),
                  ),
                ),
                child: const Text('保存修改'),
              ),
            ],
          ),
        ),
      ),
    );
    position.dispose();
    summary.dispose();
    skills.dispose();
    if (updated != null) await widget.controller.saveContent(detail, updated);
  }
}

class _ResumeIcon extends StatelessWidget {
  const _ResumeIcon();
  @override
  Widget build(BuildContext context) => Container(
    width: 48,
    height: 48,
    decoration: BoxDecoration(
      color: SpeakUpDesign.primaryMuted,
      borderRadius: BorderRadius.circular(14),
    ),
    child: const Icon(Icons.description_rounded, color: SpeakUpDesign.primary),
  );
}

class _CountBadge extends StatelessWidget {
  const _CountBadge({required this.count});
  final int count;
  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
    decoration: const BoxDecoration(
      color: SpeakUpDesign.primaryMuted,
      borderRadius: BorderRadius.all(Radius.circular(99)),
    ),
    child: Text(
      '$count / 3',
      style: SpeakUpDesign.label.copyWith(color: SpeakUpDesign.primary),
    ),
  );
}

class _EmptyResumeCard extends StatelessWidget {
  const _EmptyResumeCard({required this.onUpload});
  final VoidCallback onUpload;
  @override
  Widget build(BuildContext context) => Card(
    child: Padding(
      padding: const EdgeInsets.all(SpeakUpDesign.space24),
      child: Column(
        children: [
          const _ResumeIcon(),
          const SizedBox(height: SpeakUpDesign.space16),
          Text('从第一份简历开始', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: SpeakUpDesign.space8),
          const Text(
            '上传 10 MiB 以内的文本型 PDF，我们会提取岗位、经历与技能。',
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: SpeakUpDesign.space20),
          FilledButton.icon(
            key: const Key('resume-empty-upload-button'),
            onPressed: onUpload,
            icon: const Icon(Icons.upload_file_rounded),
            label: const Text('选择 PDF'),
          ),
        ],
      ),
    ),
  );
}

class _ResumeCard extends StatelessWidget {
  const _ResumeCard({
    required this.resume,
    required this.busy,
    required this.onOpen,
    required this.onRename,
    required this.onReplace,
    required this.onRetry,
    required this.onDelete,
  });
  final ResumeItem resume;
  final bool busy;
  final VoidCallback onOpen;
  final VoidCallback onRename;
  final VoidCallback onReplace;
  final VoidCallback? onRetry;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) => Card(
    key: Key('resume-card-${resume.id}'),
    child: InkWell(
      borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
      onTap: busy ? null : onOpen,
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const _ResumeIcon(),
            const SizedBox(width: SpeakUpDesign.space12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    resume.title,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: SpeakUpDesign.space8),
                  _StatusPill(status: resume.parseStatus),
                  const SizedBox(height: SpeakUpDesign.space8),
                  Text(
                    '${_formatBytes(resume.sizeBytes)} · ${_formatDate(resume.updatedAt)}',
                    style: SpeakUpDesign.meta,
                  ),
                ],
              ),
            ),
            if (busy)
              const Padding(
                padding: EdgeInsets.all(12),
                child: SizedBox.square(
                  dimension: 20,
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
              )
            else
              PopupMenuButton<String>(
                tooltip: '更多操作',
                onSelected: (value) => switch (value) {
                  'rename' => onRename(),
                  'replace' => onReplace(),
                  'retry' => onRetry?.call(),
                  'delete' => onDelete(),
                  _ => null,
                },
                itemBuilder: (_) => [
                  const PopupMenuItem(value: 'rename', child: Text('重命名')),
                  const PopupMenuItem(value: 'replace', child: Text('替换 PDF')),
                  if (onRetry != null)
                    const PopupMenuItem(value: 'retry', child: Text('重新解析')),
                  const PopupMenuDivider(),
                  const PopupMenuItem(value: 'delete', child: Text('删除')),
                ],
              ),
          ],
        ),
      ),
    ),
  );
}

class _StatusPill extends StatelessWidget {
  const _StatusPill({required this.status});
  final ResumeParseStatus status;
  @override
  Widget build(BuildContext context) {
    final (label, color, background, icon) = switch (status) {
      ResumeParseStatus.ready => (
        '已解析',
        SpeakUpDesign.success,
        SpeakUpDesign.successMuted,
        Icons.check_circle_rounded,
      ),
      ResumeParseStatus.failed => (
        '解析失败',
        SpeakUpDesign.error,
        SpeakUpDesign.errorMuted,
        Icons.error_rounded,
      ),
      ResumeParseStatus.parsing => (
        '解析中',
        SpeakUpDesign.primary,
        SpeakUpDesign.primaryMuted,
        Icons.autorenew_rounded,
      ),
      ResumeParseStatus.queued => (
        '等待解析',
        SpeakUpDesign.secondary,
        SpeakUpDesign.surfaceMuted,
        Icons.schedule_rounded,
      ),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 5),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(99),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 14, color: color),
          const SizedBox(width: 4),
          Text(label, style: SpeakUpDesign.meta.copyWith(color: color)),
        ],
      ),
    );
  }
}

class _ParsingCard extends StatelessWidget {
  const _ParsingCard({required this.status});
  final ResumeParseStatus status;
  @override
  Widget build(BuildContext context) => Card(
    child: Padding(
      padding: const EdgeInsets.all(SpeakUpDesign.space20),
      child: Row(
        children: [
          if (status == ResumeParseStatus.failed)
            const Icon(Icons.error_outline_rounded, color: SpeakUpDesign.error)
          else
            const SizedBox.square(
              dimension: 22,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
          const SizedBox(width: SpeakUpDesign.space12),
          Expanded(
            child: Text(
              status == ResumeParseStatus.failed
                  ? '解析未完成。如果这是扫描件或图片型 PDF，请先导出带可选中文本的 PDF，再返回列表替换文件。'
                  : '正在提取岗位、经历与技能，请稍后刷新。',
            ),
          ),
        ],
      ),
    ),
  );
}

class _ResumeContentView extends StatelessWidget {
  const _ResumeContentView({required this.content});
  final ResumeContent content;
  @override
  Widget build(BuildContext context) => Column(
    children: [
      _ContentSection(
        icon: Icons.work_outline_rounded,
        title: '目标岗位',
        body: content.targetPosition.isEmpty ? '暂未填写' : content.targetPosition,
      ),
      const SizedBox(height: SpeakUpDesign.space12),
      _ContentSection(
        icon: Icons.person_outline_rounded,
        title: '个人简介',
        body: content.professionalSummary.isEmpty
            ? '暂未填写'
            : content.professionalSummary,
      ),
      const SizedBox(height: SpeakUpDesign.space12),
      _ContentSection(
        icon: Icons.auto_awesome_rounded,
        title: '技能',
        body: content.skills.isEmpty ? '暂未填写' : content.skills.join(' · '),
      ),
      if (content.workExperiences.isNotEmpty) ...[
        const SizedBox(height: SpeakUpDesign.space12),
        _ContentSection(
          icon: Icons.business_center_outlined,
          title: '工作经历',
          body: _summarize(content.workExperiences, const [
            'company',
            'position',
          ]),
        ),
      ],
      if (content.projectExperiences.isNotEmpty) ...[
        const SizedBox(height: SpeakUpDesign.space12),
        _ContentSection(
          icon: Icons.rocket_launch_outlined,
          title: '项目经历',
          body: _summarize(content.projectExperiences, const [
            'project_name',
            'role',
          ]),
        ),
      ],
      if (content.educationExperiences.isNotEmpty) ...[
        const SizedBox(height: SpeakUpDesign.space12),
        _ContentSection(
          icon: Icons.school_outlined,
          title: '教育经历',
          body: _summarize(content.educationExperiences, const [
            'school',
            'major',
            'degree',
          ]),
        ),
      ],
    ],
  );
}

class _ContentSection extends StatelessWidget {
  const _ContentSection({
    required this.icon,
    required this.title,
    required this.body,
  });
  final IconData icon;
  final String title;
  final String body;
  @override
  Widget build(BuildContext context) => Card(
    child: Padding(
      padding: const EdgeInsets.all(SpeakUpDesign.space16),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 20, color: SpeakUpDesign.primary),
          const SizedBox(width: SpeakUpDesign.space12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: Theme.of(context).textTheme.titleMedium),
                const SizedBox(height: SpeakUpDesign.space8),
                Text(body),
              ],
            ),
          ),
        ],
      ),
    ),
  );
}

class _ErrorCard extends StatelessWidget {
  const _ErrorCard({required this.message, required this.onRetry});
  final String message;
  final VoidCallback onRetry;
  @override
  Widget build(BuildContext context) => Card(
    color: SpeakUpDesign.errorMuted,
    child: Padding(
      padding: const EdgeInsets.all(SpeakUpDesign.space16),
      child: Row(
        children: [
          const Icon(Icons.wifi_off_rounded, color: SpeakUpDesign.error),
          const SizedBox(width: 12),
          Expanded(child: Text(message)),
          TextButton(onPressed: onRetry, child: const Text('重试')),
        ],
      ),
    ),
  );
}

String _formatBytes(int bytes) => bytes < 1024 * 1024
    ? '${(bytes / 1024).toStringAsFixed(0)} KB'
    : '${(bytes / 1024 / 1024).toStringAsFixed(1)} MB';

String _formatDate(DateTime value) {
  final local = value.toLocal();
  return '${local.year}.${local.month.toString().padLeft(2, '0')}.${local.day.toString().padLeft(2, '0')}';
}

String _summarize(List<Map<String, Object?>> items, List<String> keys) => items
    .map(
      (item) => keys
          .map((key) => item[key])
          .whereType<String>()
          .where((value) => value.isNotEmpty)
          .join(' · '),
    )
    .where((value) => value.isNotEmpty)
    .join('\n');
