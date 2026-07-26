/// Review module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
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
      backgroundColor: const Color(0xFFF3F3F0),
      appBar: widget.showBackButton
          ? AppBar(
              backgroundColor: const Color(0xFFF3F3F0),
              surfaceTintColor: Colors.transparent,
              elevation: 0,
              scrolledUnderElevation: 0,
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
              if (widget.agentController?.mediaErrorMessage case final message?)
                SliverPadding(
                  padding: const EdgeInsets.fromLTRB(20, 12, 20, 0),
                  sliver: SliverToBoxAdapter(
                    child: Text(
                      message,
                      key: const Key('review-media-error-message'),
                      style: const TextStyle(color: Color(0xFF8B2E26)),
                    ),
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
          const Text(
            '复盘',
            style: TextStyle(fontSize: 32, fontWeight: FontWeight.w800),
          ),
          const SizedBox(height: 8),
          Text(
            previewMode
                ? '本地 UI Mock；复盘结果不会写入正式服务。'
                : '查看每次练习的正式结果，并把下一步改进留给下一次练习。',
            style: const TextStyle(
              color: Color(0xFF696B73),
              fontSize: 15,
              height: 1.45,
            ),
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

  String get statusLabel => isCurrent ? '刚完成' : '已完成';
  String get dateLabel =>
      completedAt == null ? '刚刚' : _compactDateLabel(completedAt!);
  String get detailDateLabel =>
      completedAt == null ? '刚刚完成' : _detailDateLabel(completedAt!);
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
    return Semantics(
      key: primary ? const Key('review-content') : null,
      button: true,
      label: '${review.title}，${entry.dateLabel}，${entry.statusLabel}，查看复盘详情',
      child: Card(
        key: Key(
          entry.isCurrent
              ? 'review-current-${review.id}'
              : 'review-history-${review.id}',
        ),
        margin: EdgeInsets.zero,
        elevation: 0,
        color: Colors.white,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(16),
          side: const BorderSide(color: Color(0xFFE4E4DF)),
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
                        style: const TextStyle(
                          fontSize: 17,
                          fontWeight: FontWeight.w700,
                          height: 1.25,
                        ),
                      ),
                      const SizedBox(height: 9),
                      Wrap(
                        spacing: 8,
                        runSpacing: 6,
                        crossAxisAlignment: WrapCrossAlignment.center,
                        children: [
                          Text(
                            entry.dateLabel,
                            key: entry.isCurrent
                                ? const Key('review-current-label')
                                : null,
                            style: const TextStyle(
                              color: Color(0xFF696B73),
                              fontSize: 13,
                            ),
                          ),
                          _StatusLabel(label: entry.statusLabel),
                        ],
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 10),
                const Icon(
                  Icons.chevron_right_rounded,
                  color: Color(0xFF6F7178),
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
        color: const Color(0xFFF0F1ED),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
        child: Text(
          label,
          style: const TextStyle(
            color: Color(0xFF55585F),
            fontSize: 12,
            fontWeight: FontWeight.w600,
          ),
        ),
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
      elevation: 0,
      color: Colors.white,
      child: Padding(
        padding: const EdgeInsets.all(22),
        child: Column(
          children: [
            Text(
              message,
              key: const Key('review-history-error'),
              textAlign: TextAlign.center,
              style: const TextStyle(color: Color(0xFF8B2E26)),
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
              style: const TextStyle(color: Color(0xFF8B2E26)),
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
    return Card(
      elevation: 0,
      color: Colors.white,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 22, vertical: 34),
        child: Column(
          children: [
            const Icon(
              Icons.fact_check_outlined,
              size: 42,
              color: Color(0xFF8B8E99),
            ),
            const SizedBox(height: 14),
            Text(
              practiceAvailable ? '完成本次练习后再来看看' : '复盘功能尚未开放',
              key: const Key('review-availability-title'),
              style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 6),
            Text(
              previewMode
                  ? '预览模式不会用当前页面状态伪造历史记录。'
                  : practiceAvailable
                  ? '完成三轮练习后，正式复盘会保存在当前账号的历史中。'
                  : '待服务端场景、语音与复盘契约开放后再接入。',
              textAlign: TextAlign.center,
              style: const TextStyle(color: Color(0xFF777983)),
            ),
          ],
        ),
      ),
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
  @override
  void initState() {
    super.initState();
    widget.entry.agentController?.addListener(_rebuild);
  }

  @override
  void didUpdateWidget(covariant _ReviewDetailPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.entry.agentController != widget.entry.agentController) {
      oldWidget.entry.agentController?.removeListener(_rebuild);
      widget.entry.agentController?.addListener(_rebuild);
    }
  }

  @override
  void dispose() {
    final controller = widget.entry.agentController;
    controller?.removeListener(_rebuild);
    unawaited(controller?.stopPracticeAudio(notify: false));
    super.dispose();
  }

  void _rebuild() {
    if (mounted) {
      setState(() {});
    }
  }

  @override
  Widget build(BuildContext context) {
    final entry = widget.entry;
    final review = entry.review;
    final controller = entry.agentController;
    return Scaffold(
      key: const Key('review-detail-page'),
      backgroundColor: const Color(0xFFF3F3F0),
      appBar: AppBar(
        backgroundColor: const Color(0xFFF3F3F0),
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        scrolledUnderElevation: 0,
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
            if (controller != null && controller.recordings.isNotEmpty) ...[
              const SizedBox(height: 12),
              PracticeRecordingsCard(controller: controller, title: '练习录音'),
            ],
            if (controller?.mediaErrorMessage case final message?) ...[
              const SizedBox(height: 12),
              Text(
                message,
                key: const Key('review-detail-media-error'),
                style: const TextStyle(color: Color(0xFF8B2E26)),
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
    return Card(
      elevation: 0,
      color: const Color(0xFFE9EAE5),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(18)),
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
                _StatusLabel(label: entry.statusLabel),
                Text(
                  entry.detailDateLabel,
                  style: const TextStyle(
                    color: Color(0xFF65676E),
                    fontSize: 13,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 14),
            Text(
              entry.review.title,
              key: const Key('review-detail-title'),
              style: const TextStyle(
                fontSize: 24,
                height: 1.2,
                fontWeight: FontWeight.w800,
              ),
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
      margin: EdgeInsets.zero,
      elevation: 0,
      color: Colors.white,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(18),
        side: const BorderSide(color: Color(0xFFE4E4DF)),
      ),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              title,
              style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 8),
            Text(
              body,
              style: const TextStyle(color: Color(0xFF55575E), height: 1.55),
            ),
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
