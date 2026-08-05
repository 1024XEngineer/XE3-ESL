/// Review reads and presents canonical Evaluation reports.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_view.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';

class ReviewPage extends StatefulWidget {
  const ReviewPage({
    this.showBackButton = false,
    this.onExit,
    this.previewMode = false,
    this.practiceAvailable = true,
    this.historyController,
    this.ieltsSpeakingReportController,
    this.ieltsSpeakingReportIndexController,
    this.autoload = true,
    super.key,
  });

  final bool showBackButton;
  final VoidCallback? onExit;
  final bool previewMode;
  final bool practiceAvailable;
  final ReviewHistoryController? historyController;
  final IeltsSpeakingReportController? ieltsSpeakingReportController;
  final IeltsSpeakingReportIndexController? ieltsSpeakingReportIndexController;
  final bool autoload;

  @override
  State<ReviewPage> createState() => _ReviewPageState();
}

class _ReviewPageState extends State<ReviewPage> {
  @override
  void initState() {
    super.initState();
    widget.historyController?.addListener(_rebuild);
    widget.ieltsSpeakingReportIndexController?.addListener(_rebuild);
    if (widget.autoload) {
      unawaited(_refresh());
    }
  }

  @override
  void didUpdateWidget(covariant ReviewPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.historyController != widget.historyController) {
      oldWidget.historyController?.removeListener(_rebuild);
      widget.historyController?.addListener(_rebuild);
      if (widget.autoload) {
        unawaited(widget.historyController?.refresh());
      }
    }
    if (oldWidget.ieltsSpeakingReportIndexController !=
        widget.ieltsSpeakingReportIndexController) {
      oldWidget.ieltsSpeakingReportIndexController?.removeListener(_rebuild);
      widget.ieltsSpeakingReportIndexController?.addListener(_rebuild);
      if (widget.autoload) {
        unawaited(widget.ieltsSpeakingReportIndexController?.refresh());
      }
    }
  }

  @override
  void dispose() {
    widget.historyController?.removeListener(_rebuild);
    widget.ieltsSpeakingReportIndexController?.removeListener(_rebuild);
    super.dispose();
  }

  void _rebuild() {
    if (mounted) setState(() {});
  }

  Future<void> _refresh() async {
    await Future.wait<void>([
      if (widget.historyController != null) widget.historyController!.refresh(),
      if (widget.ieltsSpeakingReportIndexController != null)
        widget.ieltsSpeakingReportIndexController!.refresh(),
    ]);
  }

  void _openDetail(ReviewHistoryItem item) {
    unawaited(
      Navigator.of(context).push<void>(
        MaterialPageRoute<void>(builder: (_) => _ReviewDetailPage(item: item)),
      ),
    );
  }

  void _openIeltsReport(IeltsSpeakingReportIndexItem item) {
    final controller = widget.ieltsSpeakingReportController;
    if (controller == null ||
        item.reportKind == IeltsSpeakingReportKind.interview) {
      return;
    }
    unawaited(
      Navigator.of(context).push<void>(
        MaterialPageRoute<void>(
          builder: (_) =>
              _IeltsReportDetailPage(item: item, controller: controller),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.historyController;
    final items = controller?.items ?? const <ReviewHistoryItem>[];
    final ieltsController = widget.ieltsSpeakingReportIndexController;
    final reportItems =
        ieltsController?.items ?? const <IeltsSpeakingReportIndexItem>[];
    final interviewItems = reportItems
        .where((item) => item.reportKind == IeltsSpeakingReportKind.interview)
        .toList(growable: false);
    final ieltsItems = reportItems
        .where((item) => item.reportKind == IeltsSpeakingReportKind.fullMock)
        .toList(growable: false);
    final hasItems = items.isNotEmpty || reportItems.isNotEmpty;
    final initialLoading =
        !hasItems &&
        ((controller?.isLoading ?? false) ||
            (ieltsController?.isLoading ?? false));
    final initialError = !hasItems
        ? ieltsController?.errorMessage ?? controller?.errorMessage
        : null;
    return Scaffold(
      key: const Key('review-page'),
      appBar: widget.showBackButton || widget.onExit != null
          ? AppBar(
              leading: IconButton(
                key: widget.showBackButton
                    ? const Key('review-route-back-button')
                    : const Key('review-exit-button'),
                tooltip: '返回',
                onPressed:
                    widget.onExit ?? () => Navigator.of(context).maybePop(),
                icon: const Icon(Icons.arrow_back_rounded),
              ),
            )
          : null,
      body: SafeArea(
        bottom: false,
        child: RefreshIndicator(
          onRefresh: _refresh,
          child: CustomScrollView(
            key: const Key('review-history-list'),
            physics: const AlwaysScrollableScrollPhysics(),
            slivers: [
              SliverPadding(
                padding: const EdgeInsets.fromLTRB(20, 28, 20, 0),
                sliver: SliverToBoxAdapter(
                  child: _ReviewHeader(previewMode: widget.previewMode),
                ),
              ),
              if (initialLoading)
                const SliverPadding(
                  padding: EdgeInsets.symmetric(horizontal: 20),
                  sliver: SliverToBoxAdapter(child: _HistoryLoading()),
                )
              else if (initialError != null)
                SliverPadding(
                  padding: const EdgeInsets.symmetric(horizontal: 20),
                  sliver: SliverToBoxAdapter(
                    child: _HistoryFailure(
                      message: initialError,
                      onRetry: _refresh,
                    ),
                  ),
                )
              else if (!hasItems)
                SliverPadding(
                  padding: const EdgeInsets.symmetric(horizontal: 20),
                  sliver: SliverToBoxAdapter(
                    child: _EmptyReview(
                      practiceAvailable: widget.practiceAvailable,
                      previewMode: widget.previewMode,
                    ),
                  ),
                )
              else ...[
                if (interviewItems.isNotEmpty)
                  _IeltsReportSection(
                    title: '面试练习报告',
                    items: interviewItems,
                    onOpen: _openIeltsReport,
                  ),
                if (ieltsItems.isNotEmpty)
                  _IeltsReportSection(
                    title: 'IELTS 模考报告',
                    items: ieltsItems,
                    onOpen: _openIeltsReport,
                  ),
                if (items.isNotEmpty)
                  SliverPadding(
                    padding: const EdgeInsets.symmetric(horizontal: 20),
                    sliver: SliverList(
                      delegate: SliverChildBuilderDelegate((context, index) {
                        if (index.isOdd) return const SizedBox(height: 10);
                        final item = items[index ~/ 2];
                        return _ReviewListCard(
                          item: item,
                          primary: index == 0,
                          onTap: () => _openDetail(item),
                        );
                      }, childCount: items.length * 2 - 1),
                    ),
                  ),
              ],
              if (items.isNotEmpty && controller != null)
                SliverPadding(
                  padding: const EdgeInsets.fromLTRB(20, 16, 20, 0),
                  sliver: SliverToBoxAdapter(
                    child: _HistoryFooter(controller: controller),
                  ),
                ),
              const SliverToBoxAdapter(child: SizedBox(height: 140)),
            ],
          ),
        ),
      ),
    );
  }
}

class _ReviewHeader extends StatelessWidget {
  const _ReviewHeader({required this.previewMode});

  final bool previewMode;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('复盘', style: SpeakUpDesign.pageTitle),
          const SizedBox(height: 8),
          Text(
            previewMode ? '本地预览；结果不会写入正式服务。' : '查看练习结果，明确下一次要改进的重点。',
            style: SpeakUpDesign.body,
          ),
        ],
      ),
    );
  }
}

class _IeltsReportSection extends StatelessWidget {
  const _IeltsReportSection({
    required this.title,
    required this.items,
    required this.onOpen,
  });

  final String title;
  final List<IeltsSpeakingReportIndexItem> items;
  final void Function(IeltsSpeakingReportIndexItem item) onOpen;

  @override
  Widget build(BuildContext context) {
    return SliverPadding(
      padding: const EdgeInsets.fromLTRB(20, 0, 20, 24),
      sliver: SliverList(
        delegate: SliverChildListDelegate([
          Text(title, style: SpeakUpDesign.sectionTitle),
          const SizedBox(height: 10),
          for (var index = 0; index < items.length; index++) ...[
            if (index > 0) const SizedBox(height: 10),
            _IeltsReportCard(
              item: items[index],
              onTap: () => onOpen(items[index]),
            ),
          ],
        ]),
      ),
    );
  }
}

class _IeltsReportCard extends StatelessWidget {
  const _IeltsReportCard({required this.item, required this.onTap});

  final IeltsSpeakingReportIndexItem item;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final status = switch (item.evaluationStatus) {
      IeltsSpeakingReportEvaluationStatus.queued ||
      IeltsSpeakingReportEvaluationStatus.running => '报告生成中',
      IeltsSpeakingReportEvaluationStatus.ready => '评分报告已生成',
      IeltsSpeakingReportEvaluationStatus.failed => '报告自动恢复中',
    };
    final title =
        item.title ??
        (item.reportKind == IeltsSpeakingReportKind.interview
            ? '面试复盘'
            : 'IELTS 口语完整模考');
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        key: Key('ielts-report-history-select-${item.practiceSessionId}'),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              const Icon(
                Icons.assessment_outlined,
                color: SpeakUpDesign.primary,
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title, style: SpeakUpDesign.cardTitle),
                    const SizedBox(height: 5),
                    Text(status, style: SpeakUpDesign.meta),
                  ],
                ),
              ),
              const Icon(Icons.chevron_right_rounded),
            ],
          ),
        ),
      ),
    );
  }
}

class _ReviewListCard extends StatelessWidget {
  const _ReviewListCard({
    required this.item,
    required this.primary,
    required this.onTap,
  });

  final ReviewHistoryItem item;
  final bool primary;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final review = item.review;
    final date = _compactDateLabel(item.completedAt);
    final status = _statusLabel(item.report);
    return Semantics(
      key: primary ? const Key('review-content') : null,
      button: true,
      excludeSemantics: true,
      label: '${review.title}，摘要：${review.summary}，$date，$status，查看复盘详情',
      onTap: onTap,
      child: Card(
        key: Key('review-history-${review.id}'),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          key: Key('review-history-select-${review.id}'),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 15, 12, 15),
            child: Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        review.title,
                        key: primary ? const Key('review-title') : null,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: SpeakUpDesign.cardTitle,
                      ),
                      const SizedBox(height: 6),
                      Text(
                        review.summary,
                        key: Key('review-list-summary-${review.id}'),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: SpeakUpDesign.body,
                      ),
                      const SizedBox(height: 8),
                      Wrap(
                        spacing: 8,
                        runSpacing: 6,
                        children: [
                          Text(date, style: SpeakUpDesign.meta),
                          _StatusLabel(label: status),
                        ],
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 10),
                const Icon(
                  Icons.chevron_right_rounded,
                  color: SpeakUpDesign.secondary,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _StatusLabel extends StatelessWidget {
  const _StatusLabel({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: SpeakUpDesign.primaryMuted,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
        child: Text(label, style: SpeakUpDesign.meta),
      ),
    );
  }
}

class _HistoryLoading extends StatelessWidget {
  const _HistoryLoading();

  @override
  Widget build(BuildContext context) {
    return const Padding(
      key: Key('review-history-initial-loading'),
      padding: EdgeInsets.symmetric(vertical: 72),
      child: Center(child: CircularProgressIndicator()),
    );
  }
}

class _HistoryFailure extends StatelessWidget {
  const _HistoryFailure({required this.message, required this.onRetry});

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: const Key('review-history-error'),
      padding: const EdgeInsets.symmetric(vertical: 48),
      child: Column(
        children: [
          Text(message, textAlign: TextAlign.center, style: SpeakUpDesign.body),
          const SizedBox(height: 16),
          FilledButton(
            key: const Key('review-history-retry'),
            onPressed: () => unawaited(onRetry()),
            child: const Text('重试'),
          ),
        ],
      ),
    );
  }
}

class _HistoryFooter extends StatelessWidget {
  const _HistoryFooter({required this.controller});

  final ReviewHistoryController controller;

  @override
  Widget build(BuildContext context) {
    final error = controller.errorMessage;
    if (error != null) {
      return Column(
        key: const Key('review-history-page-error'),
        children: [
          Text(error, textAlign: TextAlign.center, style: SpeakUpDesign.meta),
          TextButton(
            key: const Key('review-history-page-retry'),
            onPressed: () => unawaited(controller.retryLastFailure()),
            child: const Text('重试'),
          ),
        ],
      );
    }
    if (controller.isLoading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (!controller.hasMore) return const SizedBox.shrink();
    return Center(
      child: TextButton(
        key: const Key('review-history-load-more'),
        onPressed: () => unawaited(controller.loadMore()),
        child: const Text('加载更多'),
      ),
    );
  }
}

class _EmptyReview extends StatelessWidget {
  const _EmptyReview({
    required this.practiceAvailable,
    required this.previewMode,
  });

  final bool practiceAvailable;
  final bool previewMode;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 52),
      child: Column(
        children: [
          const Icon(Icons.history_edu_rounded, size: 44),
          const SizedBox(height: 16),
          Text(
            '完成练习后，这里会出现复盘',
            key: const Key('review-availability-title'),
            style: SpeakUpDesign.cardTitle,
          ),
          const SizedBox(height: 8),
          Text(
            previewMode
                ? '预览模式不会生成正式结果。'
                : practiceAvailable
                ? '完成一次正式练习后即可查看。'
                : '当前没有可用练习。',
            textAlign: TextAlign.center,
            style: SpeakUpDesign.body,
          ),
        ],
      ),
    );
  }
}

class _IeltsReportDetailPage extends StatefulWidget {
  const _IeltsReportDetailPage({required this.item, required this.controller});

  final IeltsSpeakingReportIndexItem item;
  final IeltsSpeakingReportController controller;

  @override
  State<_IeltsReportDetailPage> createState() => _IeltsReportDetailPageState();
}

class _IeltsReportDetailPageState extends State<_IeltsReportDetailPage> {
  @override
  void initState() {
    super.initState();
    unawaited(widget.controller.load(widget.item.practiceSessionId));
  }

  @override
  void dispose() {
    widget.controller.cancel(widget.item.practiceSessionId);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('ielts-speaking-report-detail-page'),
      appBar: AppBar(title: const Text('IELTS 口语模考报告')),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(20, 20, 20, 40),
          child: IeltsSpeakingReportPanel(controller: widget.controller),
        ),
      ),
    );
  }
}

class _ReviewDetailPage extends StatelessWidget {
  const _ReviewDetailPage({required this.item});

  final ReviewHistoryItem item;

  @override
  Widget build(BuildContext context) {
    final report = item.report;
    final findings = report.dimensions
        .expand((dimension) => dimension.improvements)
        .toList(growable: false);
    return Scaffold(
      key: const Key('review-detail-page'),
      appBar: AppBar(
        title: const Text('复盘详情'),
        leading: IconButton(
          key: const Key('review-detail-back'),
          tooltip: '返回复盘历史',
          onPressed: () => Navigator.of(context).maybePop(),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: SafeArea(
        top: false,
        child: ListView(
          key: const Key('review-detail-content'),
          padding: const EdgeInsets.fromLTRB(20, 12, 20, 48),
          children: [
            _ReviewDetailHeader(item: item),
            const SizedBox(height: 12),
            _ReviewDetailSection(
              key: const Key('review-detail-summary'),
              title: '整体表现',
              body: report.summary,
            ),
            if (report.scoreability ==
                EvaluationReportScoreability.insufficient) ...[
              const SizedBox(height: 12),
              const _ReviewStatusNotice(),
            ],
            const SizedBox(height: 12),
            _ReviewDimensions(dimensions: report.dimensions),
            if (findings.isNotEmpty) ...[
              const SizedBox(height: 12),
              _ReviewFindings(report: report, findings: findings),
            ],
          ],
        ),
      ),
    );
  }
}

class _ReviewDetailHeader extends StatelessWidget {
  const _ReviewDetailHeader({required this.item});

  final ReviewHistoryItem item;

  @override
  Widget build(BuildContext context) {
    return Card(
      color: SpeakUpDesign.primaryMuted,
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Wrap(
              spacing: 8,
              children: [
                _StatusLabel(label: _statusLabel(item.report)),
                Text(
                  _detailDateLabel(item.completedAt),
                  style: SpeakUpDesign.meta,
                ),
              ],
            ),
            const SizedBox(height: 14),
            Text(
              item.review.title,
              key: const Key('review-detail-title'),
              style: SpeakUpDesign.sectionTitle.copyWith(fontSize: 24),
            ),
          ],
        ),
      ),
    );
  }
}

class _ReviewDetailSection extends StatelessWidget {
  const _ReviewDetailSection({
    required this.title,
    required this.body,
    super.key,
  });

  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 8),
            Text(body, style: SpeakUpDesign.body),
          ],
        ),
      ),
    );
  }
}

class _ReviewStatusNotice extends StatelessWidget {
  const _ReviewStatusNotice();

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('review-detail-status-notice'),
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('本次暂不评分', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 6),
            Text('本次有效证据不足，暂不形成能力结论。完成更多回答后可以重新评估。', style: SpeakUpDesign.body),
          ],
        ),
      ),
    );
  }
}

class _ReviewDimensions extends StatelessWidget {
  const _ReviewDimensions({required this.dimensions});

  final List<EvaluationReportDimension> dimensions;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('review-detail-dimensions'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('分项表现', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 14),
            for (var index = 0; index < dimensions.length; index++) ...[
              if (index > 0) ...[
                const SizedBox(height: 16),
                const Divider(height: 1),
                const SizedBox(height: 16),
              ],
              _ReviewDimensionRow(dimension: dimensions[index]),
            ],
          ],
        ),
      ),
    );
  }
}

class _ReviewDimensionRow extends StatelessWidget {
  const _ReviewDimensionRow({required this.dimension});

  final EvaluationReportDimension dimension;

  @override
  Widget build(BuildContext context) {
    final score = dimension.score;
    final strengths = dimension.strengths.map((item) => item.message).join('；');
    return Column(
      key: Key('review-dimension-${dimension.key}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Expanded(
              child: Text(
                _dimensionLabel(dimension.key),
                style: SpeakUpDesign.label,
              ),
            ),
            if (score != null)
              Text(
                _scoreLabel(score, dimension.scale),
                style: SpeakUpDesign.label,
              ),
          ],
        ),
        if (strengths.isNotEmpty) ...[
          const SizedBox(height: 6),
          Text(strengths, style: SpeakUpDesign.body),
        ],
      ],
    );
  }
}

class _ReviewFindings extends StatelessWidget {
  const _ReviewFindings({required this.report, required this.findings});

  final EvaluationReport report;
  final List<EvaluationReportFinding> findings;

  @override
  Widget build(BuildContext context) {
    final priorityIds = report.priorityActions
        .map((item) => item.findingId)
        .toSet();
    return Card(
      key: const Key('review-detail-feedback'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('改进建议', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 14),
            for (var index = 0; index < findings.length; index++) ...[
              if (index > 0) const Divider(height: 24),
              Column(
                key: Key('review-feedback-${findings[index].id}'),
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (priorityIds.contains(findings[index].id))
                    const _StatusLabel(label: '优先练习'),
                  if (priorityIds.contains(findings[index].id))
                    const SizedBox(height: 8),
                  Text(findings[index].message, style: SpeakUpDesign.body),
                  if (findings[index].suggestion case final suggestion?) ...[
                    if (suggestion != findings[index].message) ...[
                      const SizedBox(height: 6),
                      Text('建议：$suggestion', style: SpeakUpDesign.body),
                    ],
                  ],
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }
}

String _statusLabel(EvaluationReport report) =>
    report.scoreability == EvaluationReportScoreability.insufficient
    ? '证据不足'
    : '已完成';

String _scoreLabel(double value, EvaluationReportScoreScale scale) {
  final formatted = value == value.roundToDouble()
      ? value.toInt().toString()
      : value.toStringAsFixed(1);
  return scale == EvaluationReportScoreScale.ieltsBand
      ? '$formatted / 9'
      : '$formatted / 100';
}

String _dimensionLabel(String key) {
  return const <String, String>{
        'INTERVIEW_RELEVANCE': '回答相关性',
        'INTERVIEW_STRUCTURE': '回答结构',
        'INTERVIEW_EVIDENCE': '证据与说服力',
        'INTERVIEW_PROFESSIONAL': '职业表达',
        'INTERVIEW_INTERACTION': '追问应对能力',
        'FLUENCY_COHERENCE': '流利度与连贯性',
        'LEXICAL_RESOURCE': '词汇资源',
        'GRAMMATICAL_RANGE_ACCURACY': '语法范围与准确性',
        'PRONUNCIATION': '发音',
      }[key] ??
      key.toLowerCase().replaceAll('_', ' ');
}

String _compactDateLabel(DateTime value) {
  final local = value.toLocal();
  String twoDigits(int number) => number.toString().padLeft(2, '0');
  return '${local.year}-${twoDigits(local.month)}-${twoDigits(local.day)}';
}

String _detailDateLabel(DateTime value) {
  final local = value.toLocal();
  String twoDigits(int number) => number.toString().padLeft(2, '0');
  return '${local.year}年${local.month}月${local.day}日 '
      '${twoDigits(local.hour)}:${twoDigits(local.minute)}';
}
