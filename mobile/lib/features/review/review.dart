/// Review module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/review/interview_report_view.dart';
import 'package:speakup/features/review/ielts_speaking_report_view.dart';
import 'package:speakup/practice/practice_recordings.dart';
import 'package:speakup/review/formal_review.dart';
import 'package:speakup/review/interview_report_client.dart';
import 'package:speakup/review/interview_report_controller.dart';
import 'package:speakup/review/ielts_speaking_report_controller.dart';
import 'package:speakup/review/ielts_speaking_report.dart';
import 'package:speakup/review/ielts_speaking_report_index.dart';
import 'package:speakup/review/ielts_speaking_report_index_controller.dart';
import 'package:speakup/review/review_history_client.dart';
import 'package:speakup/review/review_history_controller.dart';

class ReviewPage extends StatefulWidget {
  const ReviewPage({
    this.showBackButton = false,
    this.previewMode = false,
    this.practiceAvailable = true,
    this.historyController,
    this.agentController,
    this.interviewReportController,
    this.ieltsSpeakingReportController,
    this.ieltsSpeakingReportIndexController,
    this.autoload = true,
    super.key,
  });

  final bool showBackButton;
  final bool previewMode;
  final bool practiceAvailable;
  final ReviewHistoryController? historyController;
  final AgentController? agentController;
  final InterviewReportController? interviewReportController;
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
    widget.agentController?.addListener(_rebuild);
    if (widget.autoload) {
      unawaited(_refresh());
    }
  }

  @override
  void didUpdateWidget(covariant ReviewPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.historyController == widget.historyController) {
    } else {
      oldWidget.historyController?.removeListener(_rebuild);
      widget.historyController?.addListener(_rebuild);
      if (widget.autoload) {
        unawaited(widget.historyController?.refresh());
      }
    }
    if (oldWidget.agentController != widget.agentController) {
      oldWidget.agentController?.removeListener(_rebuild);
      widget.agentController?.addListener(_rebuild);
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
    widget.agentController?.removeListener(_rebuild);
    unawaited(widget.agentController?.stopPracticeAudio(notify: false));
    super.dispose();
  }

  void _rebuild() {
    if (mounted) {
      setState(() {});
    }
  }

  Future<void> _refresh() async {
    await Future.wait<void>([
      if (widget.historyController != null) widget.historyController!.refresh(),
      if (widget.ieltsSpeakingReportIndexController != null)
        widget.ieltsSpeakingReportIndexController!.refresh(),
    ]);
  }

  void _openDetail(_ReviewListEntry entry) {
    unawaited(
      Navigator.of(context).push<void>(
        MaterialPageRoute<void>(
          builder: (_) => _ReviewDetailPage(
            entry: entry,
            interviewReportController: widget.interviewReportController,
          ),
        ),
      ),
    );
  }

  void _openIeltsReport(IeltsSpeakingReportIndexItem item) {
    if (item.reportKind == IeltsSpeakingReportKind.interview) {
      final controller = widget.interviewReportController;
      if (controller == null) return;
      unawaited(
        Navigator.of(context).push<void>(
          MaterialPageRoute<void>(
            builder: (_) => InterviewReportPage(
              practiceSessionId: item.practiceSessionId,
              controller: controller,
            ),
          ),
        ),
      );
      return;
    }
    final reportController = widget.ieltsSpeakingReportController;
    if (reportController == null) {
      return;
    }
    unawaited(
      Navigator.of(context).push<void>(
        MaterialPageRoute<void>(
          builder: (_) =>
              _IeltsReportDetailPage(item: item, controller: reportController),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.historyController;
    final ieltsIndexController = widget.ieltsSpeakingReportIndexController;
    final ieltsItems =
        ieltsIndexController?.items ?? const <IeltsSpeakingReportIndexItem>[];
    final currentReview = widget.agentController?.review;
    final showCurrentReview =
        controller != null &&
        currentReview != null &&
        !controller.items.any((item) => item.review.id == currentReview.id);
    final entries = <_ReviewListEntry>[
      if (showCurrentReview)
        _ReviewListEntry.current(
          review: currentReview,
          formalReview: widget.agentController?.formalReview,
          agentController: widget.agentController!,
        ),
      if (controller != null)
        for (final item in controller.items)
          _ReviewListEntry.history(
            item: item,
            agentController:
                widget.agentController?.review?.id == item.review.id
                ? widget.agentController
                : null,
          ),
      if (controller == null && currentReview != null)
        _ReviewListEntry.current(
          review: currentReview,
          formalReview: widget.agentController?.formalReview,
          agentController: widget.agentController!,
        ),
    ];
    final hasReviewEntries = entries.isNotEmpty;
    final hasIeltsEntries = ieltsItems.isNotEmpty;
    final hasEntries = hasReviewEntries || hasIeltsEntries;
    final initialLoading =
        !hasEntries &&
        ((controller?.isLoading ?? false) ||
            (ieltsIndexController?.isLoading ?? false));
    final initialError =
        ieltsIndexController?.errorMessage ?? controller?.errorMessage;
    return Scaffold(
      key: const Key('review-page'),
      appBar: widget.showBackButton
          ? AppBar(
              leading: IconButton(
                key: const Key('review-route-back-button'),
                tooltip: '返回',
                onPressed: () => Navigator.of(context).maybePop(),
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
              else if (!hasEntries && initialError != null)
                SliverPadding(
                  padding: const EdgeInsets.symmetric(horizontal: 20),
                  sliver: SliverToBoxAdapter(
                    child: _HistoryFailure(
                      message: initialError,
                      onRetry: _refresh,
                    ),
                  ),
                )
              else if (!hasEntries)
                SliverPadding(
                  padding: const EdgeInsets.symmetric(horizontal: 20),
                  sliver: SliverToBoxAdapter(
                    child: _EmptyReview(
                      practiceAvailable: widget.practiceAvailable,
                      previewMode: widget.previewMode,
                    ),
                  ),
                )
              else if (hasIeltsEntries) ...[
                SliverPadding(
                  padding: const EdgeInsets.fromLTRB(20, 0, 20, 10),
                  sliver: const SliverToBoxAdapter(
                    child: Text(
                      'IELTS 模考报告',
                      style: SpeakUpDesign.sectionTitle,
                    ),
                  ),
                ),
                SliverPadding(
                  padding: const EdgeInsets.symmetric(horizontal: 20),
                  sliver: SliverList(
                    delegate: SliverChildBuilderDelegate((context, index) {
                      if (index.isOdd) {
                        return const SizedBox(height: 10);
                      }
                      final item = ieltsItems[index ~/ 2];
                      return _IeltsReportListCard(
                        item: item,
                        onTap: widget.ieltsSpeakingReportController == null
                            ? null
                            : () => _openIeltsReport(item),
                      );
                    }, childCount: ieltsItems.length * 2 - 1),
                  ),
                ),
              ],
              if (ieltsIndexController != null &&
                  (hasIeltsEntries ||
                      ieltsIndexController.isLoading ||
                      ieltsIndexController.errorMessage != null))
                SliverPadding(
                  padding: const EdgeInsets.fromLTRB(20, 16, 20, 0),
                  sliver: SliverToBoxAdapter(
                    child: _IeltsHistoryFooter(
                      controller: ieltsIndexController,
                    ),
                  ),
                ),
              if (hasReviewEntries) ...[
                if (hasIeltsEntries)
                  SliverPadding(
                    padding: const EdgeInsets.fromLTRB(20, 24, 20, 10),
                    sliver: const SliverToBoxAdapter(
                      child: Text('其他练习复盘', style: SpeakUpDesign.sectionTitle),
                    ),
                  ),
                SliverPadding(
                  padding: const EdgeInsets.symmetric(horizontal: 20),
                  sliver: SliverList(
                    delegate: SliverChildBuilderDelegate((context, index) {
                      if (index.isOdd) {
                        return const SizedBox(height: 10);
                      }
                      final entryIndex = index ~/ 2;
                      final entry = entries[entryIndex];
                      return _ReviewListCard(
                        entry: entry,
                        primary: entryIndex == 0,
                        onTap: () => _openDetail(entry),
                      );
                    }, childCount: entries.length * 2 - 1),
                  ),
                ),
              ],
              if (hasReviewEntries && controller != null)
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
          SpeakUpPageHeader(
            title: '复盘',
            subtitle: previewMode ? '本地预览；结果不会写入正式服务。' : '查看练习结果，明确下一次要改进的重点。',
          ),
        ],
      ),
    );
  }
}

final class _ReviewListEntry {
  const _ReviewListEntry._({
    required this.review,
    required this.isCurrent,
    this.formalReview,
    this.completedAt,
    this.agentController,
  });

  factory _ReviewListEntry.current({
    required AgentReview review,
    FormalReview? formalReview,
    required AgentController agentController,
  }) {
    return _ReviewListEntry._(
      review: review,
      formalReview: formalReview,
      isCurrent: true,
      agentController: agentController,
    );
  }

  factory _ReviewListEntry.history({
    required ReviewHistoryItem item,
    AgentController? agentController,
  }) {
    return _ReviewListEntry._(
      review: item.review,
      formalReview: item.formalReview,
      completedAt: item.completedAt,
      isCurrent: false,
      agentController: agentController,
    );
  }

  final AgentReview review;
  final FormalReview? formalReview;
  final DateTime? completedAt;
  final bool isCurrent;
  final AgentController? agentController;

  String get statusLabel {
    final eligibility = formalReview?.result?.eligibility;
    return switch (eligibility) {
      FormalReviewSummaryEligibility.provisional => '面试能力反馈',
      FormalReviewSummaryEligibility.insufficientEvidence => '证据不足',
      _ => isCurrent ? '本次结果' : '已完成',
    };
  }

  String? get dateLabel =>
      completedAt == null ? null : _compactDateLabel(completedAt!);
  String? get detailDateLabel =>
      completedAt == null ? null : _detailDateLabel(completedAt!);
}

class _ReviewListCard extends StatelessWidget {
  const _ReviewListCard({
    required this.entry,
    required this.primary,
    required this.onTap,
  });

  final _ReviewListEntry entry;
  final bool primary;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final review = entry.review;
    final dateLabel = entry.dateLabel;
    final semanticsLabel = <String>[
      review.title,
      '摘要：${review.summary}',
      ?dateLabel,
      entry.statusLabel,
      '查看复盘详情',
    ].join('，');
    return Semantics(
      key: primary ? const Key('review-content') : null,
      button: true,
      excludeSemantics: true,
      label: semanticsLabel,
      onTap: onTap,
      child: Card(
        key: Key(
          entry.isCurrent
              ? 'review-current-${review.id}'
              : 'review-history-${review.id}',
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          key: Key(
            entry.isCurrent
                ? 'review-current-select-${review.id}'
                : 'review-history-select-${review.id}',
          ),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 15, 12, 15),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        review.title,
                        key: primary
                            ? const Key('review-title')
                            : Key(
                                entry.isCurrent
                                    ? 'review-current-title-${review.id}'
                                    : 'review-history-title-${review.id}',
                              ),
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
                        crossAxisAlignment: WrapCrossAlignment.center,
                        children: [
                          if (dateLabel != null)
                            Text(dateLabel, style: SpeakUpDesign.meta),
                          _StatusLabel(
                            key: entry.isCurrent
                                ? const Key('review-current-label')
                                : null,
                            label: entry.statusLabel,
                          ),
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

class _IeltsReportListCard extends StatelessWidget {
  const _IeltsReportListCard({required this.item, required this.onTap});

  final IeltsSpeakingReportIndexItem item;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final statusLabel = switch (item.evaluationStatus) {
      IeltsSpeakingReportEvaluationStatus.queued ||
      IeltsSpeakingReportEvaluationStatus.running => '报告生成中',
      IeltsSpeakingReportEvaluationStatus.ready => '部分练习报告',
      IeltsSpeakingReportEvaluationStatus.failed => '报告生成失败',
    };
    final dateLabel = _compactDateLabel(item.updatedAt);
    return Semantics(
      button: onTap != null,
      excludeSemantics: true,
      label: 'IELTS 口语完整模考，$statusLabel，$dateLabel，查看报告',
      onTap: onTap,
      child: Card(
        key: Key('ielts-report-history-${item.practiceSessionId}'),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          key: Key('ielts-report-history-select-${item.practiceSessionId}'),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 15, 12, 15),
            child: Row(
              children: [
                Container(
                  width: 42,
                  height: 42,
                  decoration: BoxDecoration(
                    color: SpeakUpDesign.primaryMuted,
                    borderRadius: BorderRadius.circular(14),
                  ),
                  child: const Icon(
                    Icons.assessment_outlined,
                    color: SpeakUpDesign.primary,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        item.reportKind == IeltsSpeakingReportKind.interview
                            ? '面试练习报告'
                            : 'IELTS 口语完整模考',
                        style: SpeakUpDesign.cardTitle,
                      ),
                      const SizedBox(height: 5),
                      Text(
                        '$statusLabel · $dateLabel',
                        style: SpeakUpDesign.meta,
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                const Icon(Icons.chevron_right_rounded),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _StatusLabel extends StatelessWidget {
  const _StatusLabel({required this.label, super.key});

  final String label;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: SpeakUpDesign.surfaceMuted,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
        child: Text(label, style: SpeakUpDesign.meta),
      ),
    );
  }
}

class _HistoryLoading extends StatelessWidget {
  const _HistoryLoading();

  @override
  Widget build(BuildContext context) {
    return const Center(
      child: Padding(
        padding: EdgeInsets.symmetric(vertical: 48),
        child: CircularProgressIndicator(
          key: Key('review-history-initial-loading'),
        ),
      ),
    );
  }
}

class _HistoryFailure extends StatelessWidget {
  const _HistoryFailure({required this.message, required this.onRetry});

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(22),
        child: Column(
          children: [
            Text(
              message,
              key: const Key('review-history-error'),
              textAlign: TextAlign.center,
              style: const TextStyle(color: SpeakUpDesign.error),
            ),
            const SizedBox(height: 12),
            OutlinedButton(
              key: const Key('review-history-retry'),
              onPressed: onRetry,
              child: const Text('重试'),
            ),
          ],
        ),
      ),
    );
  }
}

class _InlineHistoryFailure extends StatelessWidget {
  const _InlineHistoryFailure({required this.message, required this.onRetry});

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Row(
        children: [
          Expanded(
            child: Text(
              message,
              key: const Key('review-history-page-error'),
              style: const TextStyle(color: SpeakUpDesign.error),
            ),
          ),
          TextButton(
            key: const Key('review-history-page-retry'),
            onPressed: onRetry,
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
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (controller.errorMessage case final message?)
          _InlineHistoryFailure(
            message: message,
            onRetry: controller.retryLastFailure,
          ),
        if (controller.isLoading)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Center(
              child: CircularProgressIndicator(
                key: Key('review-history-page-loading'),
              ),
            ),
          )
        else if (controller.hasMore)
          Center(
            child: OutlinedButton(
              key: const Key('review-history-load-more'),
              onPressed: controller.loadMore,
              child: const Text('加载更早的复盘'),
            ),
          ),
      ],
    );
  }
}

class _IeltsHistoryFooter extends StatelessWidget {
  const _IeltsHistoryFooter({required this.controller});

  final IeltsSpeakingReportIndexController controller;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (controller.errorMessage case final message?)
          _InlineHistoryFailure(
            message: message,
            onRetry: controller.retryLastFailure,
          ),
        if (controller.isLoading)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: Center(
              child: CircularProgressIndicator(
                key: Key('ielts-report-history-page-loading'),
              ),
            ),
          )
        else if (controller.hasMore)
          Center(
            child: OutlinedButton(
              key: const Key('ielts-report-history-load-more'),
              onPressed: controller.loadMore,
              child: const Text('加载更早的 IELTS 报告'),
            ),
          ),
      ],
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
    return SpeakUpEmptyState(
      key: const Key('review-availability-title'),
      title: practiceAvailable ? '完成练习后查看复盘' : '复盘功能尚未开放',
      message: previewMode
          ? '预览模式不会伪造历史记录。'
          : practiceAvailable
          ? '练习结束后，结果会保存在当前账号中。'
          : '正式训练服务开放后可在这里查看结果。',
      icon: Icons.fact_check_outlined,
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
    final envelope = widget.controller.envelope;
    if (widget.controller.practiceSessionId != widget.item.practiceSessionId ||
        envelope?.evaluationRevisionId != widget.item.evaluationRevisionId) {
      unawaited(widget.controller.load(widget.item.practiceSessionId));
    }
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

class _ReviewDetailPage extends StatefulWidget {
  const _ReviewDetailPage({
    required this.entry,
    required this.interviewReportController,
  });

  final _ReviewListEntry entry;
  final InterviewReportController? interviewReportController;

  @override
  State<_ReviewDetailPage> createState() => _ReviewDetailPageState();
}

class _ReviewDetailPageState extends State<_ReviewDetailPage> {
  String? _visibleMediaError;
  String? _ignoredInitialMediaError;

  @override
  void initState() {
    super.initState();
    widget.entry.agentController?.addListener(_rebuild);
    widget.interviewReportController?.addListener(_rebuild);
    _ignoredInitialMediaError = _matchingController()?.mediaErrorMessage;
    _loadInterviewReport();
  }

  @override
  void didUpdateWidget(covariant _ReviewDetailPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.entry.agentController != widget.entry.agentController) {
      oldWidget.entry.agentController?.removeListener(_rebuild);
      widget.entry.agentController?.addListener(_rebuild);
    }
    if (oldWidget.interviewReportController !=
        widget.interviewReportController) {
      oldWidget.interviewReportController?.removeListener(_rebuild);
      widget.interviewReportController?.addListener(_rebuild);
    }
    _visibleMediaError = null;
    _ignoredInitialMediaError = _matchingController()?.mediaErrorMessage;
    if (_interviewSessionId(oldWidget.entry) !=
            _interviewSessionId(widget.entry) ||
        oldWidget.interviewReportController !=
            widget.interviewReportController) {
      final oldSessionId = _interviewSessionId(oldWidget.entry);
      if (oldSessionId != null) {
        oldWidget.interviewReportController?.cancel(oldSessionId);
      }
      _loadInterviewReport();
    }
  }

  @override
  void dispose() {
    final attachedController = widget.entry.agentController;
    attachedController?.removeListener(_rebuild);
    widget.interviewReportController?.removeListener(_rebuild);
    unawaited(_matchingController()?.stopPracticeAudio(notify: false));
    final sessionId = _interviewSessionId(widget.entry);
    if (sessionId != null) {
      widget.interviewReportController?.cancel(sessionId);
    }
    super.dispose();
  }

  void _loadInterviewReport() {
    final sessionId = _interviewSessionId(widget.entry);
    if (sessionId != null) {
      unawaited(widget.interviewReportController?.load(sessionId));
    }
  }

  void _rebuild() {
    if (!mounted) {
      return;
    }
    final controller = _matchingController();
    final error = controller != null && controller.recordings.isNotEmpty
        ? controller.mediaErrorMessage
        : null;
    if (error == null) {
      _visibleMediaError = null;
      _ignoredInitialMediaError = null;
    } else if (error != _ignoredInitialMediaError) {
      _visibleMediaError = error;
    }
    setState(() {});
  }

  AgentController? _matchingController() {
    final controller = widget.entry.agentController;
    return controller?.review?.id == widget.entry.review.id ? controller : null;
  }

  @override
  Widget build(BuildContext context) {
    final entry = widget.entry;
    final review = entry.review;
    final controller = _matchingController();
    final hasRecordingControls =
        controller != null && controller.recordings.isNotEmpty;
    final mediaError = _visibleMediaError;
    final formalReview = entry.formalReview;
    final scenarioReview = formalReview?.schema == FormalReviewSchema.scenarioV2
        ? formalReview
        : null;
    final scenarioResult = scenarioReview?.result;
    final interviewSessionId = _interviewSessionId(entry);
    final interviewReportController = widget.interviewReportController;
    final showInterviewReport =
        interviewSessionId != null &&
        interviewReportController != null &&
        interviewReportController.failureKind !=
            InterviewReportFailureKind.notFound;
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
            _ReviewDetailHeader(entry: entry),
            const SizedBox(height: 12),
            if (showInterviewReport)
              InterviewReportPanel(controller: interviewReportController)
            else ...[
              _ReviewDetailSection(
                key: const Key('review-detail-summary'),
                title: '整体表现',
                body: review.summary,
              ),
              if (scenarioReview != null && scenarioResult != null) ...[
                if (_reviewNotice(scenarioReview) case final notice?) ...[
                  const SizedBox(height: 12),
                  _ReviewStatusNotice(
                    key: const Key('review-detail-status-notice'),
                    title: notice.title,
                    message: notice.message,
                  ),
                ],
                if (scenarioResult.dimensions.isNotEmpty) ...[
                  const SizedBox(height: 12),
                  _ReviewDimensions(dimensions: scenarioResult.dimensions),
                ],
                if (scenarioResult.feedbackItems.isNotEmpty) ...[
                  const SizedBox(height: 12),
                  _ReviewFeedback(
                    items: scenarioResult.feedbackItems,
                    priorityRefs: scenarioResult.repracticeSuggestionRefs,
                  ),
                ],
              ] else ...[
                const SizedBox(height: 12),
                _ReviewDetailSection(
                  key: const Key('review-detail-strength'),
                  title: '做得好的地方',
                  body: review.strength,
                ),
                const SizedBox(height: 12),
                _ReviewDetailSection(
                  key: const Key('review-detail-focus'),
                  title: '下一次重点',
                  body: review.nextFocus,
                ),
              ],
            ],
            if (hasRecordingControls) ...[
              const SizedBox(height: 12),
              PracticeRecordingsCard(controller: controller, title: '练习录音'),
            ],
            if (hasRecordingControls && mediaError != null) ...[
              const SizedBox(height: 12),
              Text(
                mediaError,
                key: const Key('review-detail-media-error'),
                style: const TextStyle(color: SpeakUpDesign.error),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

String? _interviewSessionId(_ReviewListEntry entry) {
  final review = entry.formalReview;
  if (review?.schema != FormalReviewSchema.scenarioV2 ||
      review?.contextType != FormalReviewContextType.interviewProjectDeepDive) {
    return null;
  }
  return review!.practiceSessionId;
}

class _ReviewStatusNotice extends StatelessWidget {
  const _ReviewStatusNotice({
    required this.title,
    required this.message,
    super.key,
  });

  final String title;
  final String message;

  @override
  Widget build(BuildContext context) {
    return Card(
      color: SpeakUpDesign.primaryMuted,
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 6),
            Text(message, style: SpeakUpDesign.body),
          ],
        ),
      ),
    );
  }
}

class _ReviewDimensions extends StatelessWidget {
  const _ReviewDimensions({required this.dimensions});

  final List<FormalReviewDimension> dimensions;

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

  final FormalReviewDimension dimension;

  @override
  Widget build(BuildContext context) {
    final score = dimension.score;
    return Column(
      key: Key('review-dimension-${dimension.key}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: Text(
                _dimensionLabel(dimension.category),
                style: SpeakUpDesign.label,
              ),
            ),
            if (score != null) ...[
              const SizedBox(width: 12),
              Text('$score / 100', style: SpeakUpDesign.label),
            ],
          ],
        ),
        const SizedBox(height: 6),
        Text(dimension.message, style: SpeakUpDesign.body),
        if (dimension.suggestion case final suggestion?) ...[
          const SizedBox(height: 6),
          Text('建议：$suggestion', style: SpeakUpDesign.body),
        ],
      ],
    );
  }
}

class _ReviewFeedback extends StatelessWidget {
  const _ReviewFeedback({required this.items, required this.priorityRefs});

  final List<FormalReviewFeedbackItem> items;
  final List<String> priorityRefs;

  @override
  Widget build(BuildContext context) {
    final priorities = priorityRefs.toSet();
    return Card(
      key: const Key('review-detail-feedback'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('反馈与纠错', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 14),
            for (var index = 0; index < items.length; index++) ...[
              if (index > 0) ...[
                const SizedBox(height: 16),
                const Divider(height: 1),
                const SizedBox(height: 16),
              ],
              _ReviewFeedbackRow(
                item: items[index],
                priority: priorities.contains(items[index].key),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _ReviewFeedbackRow extends StatelessWidget {
  const _ReviewFeedbackRow({required this.item, required this.priority});

  final FormalReviewFeedbackItem item;
  final bool priority;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: Key('review-feedback-${item.key}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: 8,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            _StatusLabel(label: _feedbackLabel(item.kind)),
            if (priority) const _StatusLabel(label: '优先练习'),
          ],
        ),
        const SizedBox(height: 8),
        Text(item.message, style: SpeakUpDesign.body),
        if (item.suggestion case final suggestion?) ...[
          const SizedBox(height: 6),
          Text('建议：$suggestion', style: SpeakUpDesign.body),
        ],
      ],
    );
  }
}

class _ReviewDetailHeader extends StatelessWidget {
  const _ReviewDetailHeader({required this.entry});

  final _ReviewListEntry entry;

  @override
  Widget build(BuildContext context) {
    final detailDateLabel = entry.detailDateLabel;
    return Card(
      color: SpeakUpDesign.primaryMuted,
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Wrap(
              spacing: 8,
              runSpacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                _StatusLabel(
                  key: entry.isCurrent
                      ? const Key('review-detail-current-label')
                      : null,
                  label: entry.statusLabel,
                ),
                if (detailDateLabel != null)
                  Text(detailDateLabel, style: SpeakUpDesign.meta),
              ],
            ),
            const SizedBox(height: 14),
            Text(
              entry.review.title,
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

({String title, String message})? _reviewNotice(FormalReview review) {
  final result = review.result!;
  if (result.eligibility ==
      FormalReviewSummaryEligibility.insufficientEvidence) {
    return (
      title: '本次暂不评分',
      message: _insufficientMessage(result.insufficientEvidenceReasons),
    );
  }
  if (review.contextType == FormalReviewContextType.ieltsSpeakingPart2) {
    if (result.eligibility == FormalReviewSummaryEligibility.provisional) {
      return (
        title: '面试能力反馈',
        message:
            '当前只依据已确认文字评估；发音尚未评估，因此不会生成 Overall。'
            '这是 AI 练习反馈，不是 IELTS 官方成绩。',
      );
    }
    return (title: '练习估分说明', message: '这是 AI 练习反馈，不是 IELTS 官方成绩。');
  }
  return null;
}

String _insufficientMessage(List<String> reasons) {
  if (reasons.contains('confirmed_answer_too_short')) {
    return '有效回答太短。完成一段更完整的回答后再试，本次不会按低分处理。';
  }
  return '本次有效证据不足，暂不评分。完成更多回答后可以重新评估。';
}

String _feedbackLabel(FormalReviewFeedbackKind kind) {
  return switch (kind) {
    FormalReviewFeedbackKind.correction => '纠错',
    FormalReviewFeedbackKind.strength => '做得好的地方',
    FormalReviewFeedbackKind.improvement => '改进建议',
    FormalReviewFeedbackKind.recommendedExpression => '推荐表达',
  };
}

String _dimensionLabel(String category) {
  return const <String, String>{
        'relevance_structure': '回答相关性与结构',
        'technical_depth': '专业与技术深度',
        'ownership_decisions': '主动性与决策',
        'evidence_impact': '证据与影响',
        'language_clarity': '英语表达清晰度',
        'task_coverage_development': '任务覆盖与展开',
        'coherence': '连贯与衔接',
        'lexical_resource': '词汇资源',
        'grammar_range_accuracy': '语法范围与准确性',
        'progress_clarity': '进度表达',
        'risk_specificity': '风险具体性',
        'impact_priority': '影响与优先级',
        'next_step_ask': '下一步与诉求',
        'intent_clarity': '意图清晰度',
        'information_completeness': '信息完整性',
        'politeness_tone': '礼貌与语气',
        'resolution_effectiveness': '问题解决效果',
        'task_relevance': '任务相关性',
        'clarity': '表达清晰度',
        'grammar_accuracy': '语法准确性',
        'interaction_effectiveness': '互动效果',
      }[category] ??
      category.replaceAll('_', ' ');
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
