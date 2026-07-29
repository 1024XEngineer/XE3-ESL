/// Review module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/practice/practice_recordings.dart';
import 'package:speakup/review/review_history_client.dart';
import 'package:speakup/review/review_history_controller.dart';

class ReviewPage extends StatefulWidget {
  const ReviewPage({
    this.showBackButton = false,
    this.previewMode = false,
    this.practiceAvailable = true,
    this.historyController,
    this.agentController,
    this.autoload = true,
    super.key,
  });

  final bool showBackButton;
  final bool previewMode;
  final bool practiceAvailable;
  final ReviewHistoryController? historyController;
  final AgentController? agentController;
  final bool autoload;

  @override
  State<ReviewPage> createState() => _ReviewPageState();
}

class _ReviewPageState extends State<ReviewPage> {
  @override
  void initState() {
    super.initState();
    widget.historyController?.addListener(_rebuild);
    widget.agentController?.addListener(_rebuild);
    if (widget.autoload) {
      unawaited(widget.historyController?.refresh());
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
  }

  @override
  void dispose() {
    widget.historyController?.removeListener(_rebuild);
    widget.agentController?.removeListener(_rebuild);
    unawaited(widget.agentController?.stopPracticeAudio(notify: false));
    super.dispose();
  }

  void _rebuild() {
    if (mounted) {
      setState(() {});
    }
  }

  void _openDetail(_ReviewListEntry entry) {
    unawaited(
      Navigator.of(context).push<void>(
        MaterialPageRoute<void>(
          builder: (_) => _ReviewDetailPage(entry: entry),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.historyController;
    final currentReview = widget.agentController?.review;
    final showCurrentReview =
        controller != null &&
        currentReview != null &&
        !controller.items.any((item) => item.review.id == currentReview.id);
    final entries = <_ReviewListEntry>[
      if (showCurrentReview)
        _ReviewListEntry.current(
          review: currentReview,
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
          agentController: widget.agentController!,
        ),
    ];
    final hasEntries = entries.isNotEmpty;
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
          onRefresh: controller?.refresh ?? () async {},
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
              if (!hasEntries && controller != null && controller.isLoading)
                const SliverPadding(
                  padding: EdgeInsets.symmetric(horizontal: 20),
                  sliver: SliverToBoxAdapter(child: _HistoryLoading()),
                )
              else if (!hasEntries &&
                  controller != null &&
                  controller.errorMessage != null)
                SliverPadding(
                  padding: const EdgeInsets.symmetric(horizontal: 20),
                  sliver: SliverToBoxAdapter(
                    child: _HistoryFailure(
                      message: controller.errorMessage!,
                      onRetry: controller.retryLastFailure,
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
              else
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
              if (hasEntries && controller != null)
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
    this.completedAt,
    this.agentController,
  });

  factory _ReviewListEntry.current({
    required AgentReview review,
    required AgentController agentController,
  }) {
    return _ReviewListEntry._(
      review: review,
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
      completedAt: item.completedAt,
      isCurrent: false,
      agentController: agentController,
    );
  }

  final AgentReview review;
  final DateTime? completedAt;
  final bool isCurrent;
  final AgentController? agentController;

  String get statusLabel => isCurrent ? '本次结果' : '已完成';
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

class _ReviewDetailPage extends StatefulWidget {
  const _ReviewDetailPage({required this.entry});

  final _ReviewListEntry entry;

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
    _ignoredInitialMediaError = _matchingController()?.mediaErrorMessage;
  }

  @override
  void didUpdateWidget(covariant _ReviewDetailPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.entry.agentController != widget.entry.agentController) {
      oldWidget.entry.agentController?.removeListener(_rebuild);
      widget.entry.agentController?.addListener(_rebuild);
    }
    _visibleMediaError = null;
    _ignoredInitialMediaError = _matchingController()?.mediaErrorMessage;
  }

  @override
  void dispose() {
    final attachedController = widget.entry.agentController;
    attachedController?.removeListener(_rebuild);
    unawaited(_matchingController()?.stopPracticeAudio(notify: false));
    super.dispose();
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
            _ReviewDetailSection(
              key: const Key('review-detail-summary'),
              title: '整体表现',
              body: review.summary,
            ),
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
