/// Review reads and presents canonical Evaluation reports.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/evaluation_report_detail_page.dart';
import 'package:speakup/features/coaching/review/ielts_evaluation_overview.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';

class ReviewPage extends StatefulWidget {
  const ReviewPage({
    this.showBackButton = false,
    this.onExit,
    this.previewMode = false,
    this.practiceAvailable = true,
    this.historyController,
    this.autoload = true,
    super.key,
  });

  final bool showBackButton;
  final VoidCallback? onExit;
  final bool previewMode;
  final bool practiceAvailable;
  final ReviewHistoryController? historyController;
  final bool autoload;

  @override
  State<ReviewPage> createState() => _ReviewPageState();
}

class _ReviewPageState extends State<ReviewPage> {
  @override
  void initState() {
    super.initState();
    if (widget.autoload) unawaited(_refresh());
  }

  @override
  void didUpdateWidget(covariant ReviewPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.historyController != widget.historyController &&
        widget.autoload) {
      unawaited(widget.historyController?.refresh());
    }
  }

  Future<void> _refresh() async => widget.historyController?.refresh();

  void _openDetail(ReviewHistoryItem item) {
    unawaited(
      Navigator.of(context).push<void>(
        MaterialPageRoute<void>(
          builder: (_) => ReviewReportDetailPage(item: item),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) => _ReviewHistoryPage(
    showBackButton: widget.showBackButton,
    onExit: widget.onExit,
    historyController: widget.historyController,
    practiceAvailable: widget.practiceAvailable,
    previewMode: widget.previewMode,
    onRefresh: _refresh,
    onOpenDetail: _openDetail,
  );
}

bool _isFullMockIeltsReport(EvaluationReport report) =>
    report.sceneType == EvaluationReportSceneType.ieltsSpeaking &&
    report.practiceMode == 'FULL_MOCK';

class CurrentIeltsAbilityProfile extends StatelessWidget {
  const CurrentIeltsAbilityProfile({
    required this.historyController,
    super.key,
  });

  final ReviewHistoryController? historyController;

  @override
  Widget build(BuildContext context) {
    final controller = historyController;
    if (controller == null) {
      return const _CurrentIeltsAbility(report: null, loading: false);
    }
    return AnimatedBuilder(
      animation: controller,
      builder: (context, _) {
        final item = _latestFullMock(controller.items);
        return _CurrentIeltsAbility(
          report: item?.report,
          loading: item == null && controller.isLoading,
        );
      },
    );
  }
}

class CurrentIeltsAbilityPage extends StatelessWidget {
  const CurrentIeltsAbilityPage({required this.historyController, super.key});

  final ReviewHistoryController? historyController;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('current-ielts-ability-page'),
      backgroundColor: SpeakUpDesign.surfaceMuted,
      appBar: AppBar(
        backgroundColor: SpeakUpDesign.surfaceMuted,
        leading: IconButton(
          key: const Key('current-ielts-ability-back-button'),
          tooltip: '返回',
          onPressed: () => Navigator.of(context).maybePop(),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
        title: const Text('IELTS 能力'),
      ),
      body: SafeArea(
        child: ListView(
          padding: EdgeInsets.fromLTRB(
            SpeakUpDesign.horizontalInset(context),
            SpeakUpDesign.space16,
            SpeakUpDesign.horizontalInset(context),
            SpeakUpDesign.space24,
          ),
          children: [
            CurrentIeltsAbilityProfile(historyController: historyController),
          ],
        ),
      ),
    );
  }
}

class _CurrentIeltsAbility extends StatelessWidget {
  const _CurrentIeltsAbility({required this.report, required this.loading});

  final EvaluationReport? report;
  final bool loading;

  @override
  Widget build(BuildContext context) {
    final current = report;
    if (current != null) {
      return IeltsEvaluationOverview(
        report: current,
        title: '当前 IELTS 能力',
        scoreTitle: '当前估分',
      );
    }
    return Card(
      key: const Key('review-ability-empty'),
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: loading
            ? const Center(child: CircularProgressIndicator())
            : Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('当前 IELTS 能力', style: SpeakUpDesign.cardTitle),
                  const SizedBox(height: SpeakUpDesign.space12),
                  Text('完成一次全真模考后，这里会显示四维能力与当前估分。', style: SpeakUpDesign.meta),
                ],
              ),
      ),
    );
  }
}

ReviewHistoryItem? _latestFullMock(List<ReviewHistoryItem> items) {
  ReviewHistoryItem? latest;
  for (final item in items) {
    if (!_isFullMockIeltsReport(item.report)) continue;
    if (latest == null || item.completedAt.isAfter(latest.completedAt)) {
      latest = item;
    }
  }
  return latest;
}

class _ReviewHistoryPage extends StatefulWidget {
  const _ReviewHistoryPage({
    required this.showBackButton,
    required this.onExit,
    required this.historyController,
    required this.practiceAvailable,
    required this.previewMode,
    required this.onRefresh,
    required this.onOpenDetail,
  });

  final bool showBackButton;
  final VoidCallback? onExit;
  final ReviewHistoryController? historyController;
  final bool practiceAvailable;
  final bool previewMode;
  final Future<void> Function() onRefresh;
  final ValueChanged<ReviewHistoryItem> onOpenDetail;

  @override
  State<_ReviewHistoryPage> createState() => _ReviewHistoryPageState();
}

class _ReviewHistoryPageState extends State<_ReviewHistoryPage> {
  EvaluationReportSceneType? _sceneType;
  bool _showInsufficient = false;

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: Listenable.merge(<Listenable>[?widget.historyController]),
      builder: (context, _) {
        final allItems =
            widget.historyController?.items ?? const <ReviewHistoryItem>[];
        final sceneItems = allItems
            .where(
              (item) =>
                  _sceneType == null || item.report.sceneType == _sceneType,
            )
            .toList(growable: false);
        final insufficientCount = sceneItems
            .where(
              (item) =>
                  item.report.scoreability ==
                  EvaluationReportScoreability.insufficient,
            )
            .length;
        final items = sceneItems
            .where(
              (item) =>
                  _showInsufficient ||
                  item.report.scoreability !=
                      EvaluationReportScoreability.insufficient,
            )
            .toList(growable: false);
        final hasAllItems = allItems.isNotEmpty;
        final hasItems = items.isNotEmpty;
        final initialLoading =
            !hasAllItems && (widget.historyController?.isLoading ?? false);
        final initialError = !hasAllItems
            ? widget.historyController?.errorMessage
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
              onRefresh: widget.onRefresh,
              child: CustomScrollView(
                key: const Key('review-history-list'),
                physics: const AlwaysScrollableScrollPhysics(),
                slivers: [
                  SliverPadding(
                    padding: EdgeInsets.fromLTRB(
                      SpeakUpDesign.horizontalInset(context),
                      SpeakUpDesign.space24,
                      SpeakUpDesign.horizontalInset(context),
                      0,
                    ),
                    sliver: SliverToBoxAdapter(
                      child: _ReviewHeader(previewMode: widget.previewMode),
                    ),
                  ),
                  if (hasAllItems)
                    SliverPadding(
                      padding: const EdgeInsets.fromLTRB(20, 0, 20, 8),
                      sliver: SliverToBoxAdapter(
                        child: _ReviewFilters(
                          selected: _sceneType,
                          onSelected: (value) =>
                              setState(() => _sceneType = value),
                        ),
                      ),
                    ),
                  if (hasAllItems)
                    SliverPadding(
                      padding: const EdgeInsets.fromLTRB(20, 0, 20, 8),
                      sliver: SliverToBoxAdapter(
                        child: Row(
                          children: [
                            Expanded(
                              child: Text(
                                '${items.length} 份报告',
                                style: SpeakUpDesign.sectionTitle.copyWith(
                                  fontSize: 18,
                                ),
                              ),
                            ),
                            if (insufficientCount > 0)
                              TextButton(
                                key: const Key('review-toggle-insufficient'),
                                onPressed: () => setState(
                                  () => _showInsufficient = !_showInsufficient,
                                ),
                                child: Text(
                                  _showInsufficient
                                      ? '隐藏未评分记录'
                                      : '显示未评分记录（$insufficientCount）',
                                  style: SpeakUpDesign.meta,
                                ),
                              ),
                          ],
                        ),
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
                          onRetry: widget.onRefresh,
                        ),
                      ),
                    )
                  else if (!hasAllItems)
                    SliverPadding(
                      padding: const EdgeInsets.symmetric(horizontal: 20),
                      sliver: SliverToBoxAdapter(
                        child: _EmptyReview(
                          practiceAvailable: widget.practiceAvailable,
                          previewMode: widget.previewMode,
                        ),
                      ),
                    )
                  else if (!hasItems)
                    const SliverPadding(
                      padding: EdgeInsets.symmetric(horizontal: 20),
                      sliver: SliverToBoxAdapter(child: _FilteredReviewEmpty()),
                    )
                  else ...[
                    if (items.isNotEmpty)
                      SliverPadding(
                        padding: const EdgeInsets.symmetric(horizontal: 20),
                        sliver: SliverGrid(
                          gridDelegate:
                              const SliverGridDelegateWithFixedCrossAxisCount(
                                crossAxisCount: 2,
                                crossAxisSpacing: 12,
                                mainAxisSpacing: 12,
                                mainAxisExtent: 128,
                              ),
                          delegate: SliverChildBuilderDelegate((
                            context,
                            index,
                          ) {
                            final item = items[index];
                            return _ReviewListCard(
                              item: item,
                              primary: index == 0,
                              onTap: () => widget.onOpenDetail(item),
                            );
                          }, childCount: items.length),
                        ),
                      ),
                  ],
                  if (allItems.isNotEmpty && widget.historyController != null)
                    SliverPadding(
                      padding: const EdgeInsets.fromLTRB(20, 16, 20, 0),
                      sliver: SliverToBoxAdapter(
                        child: _HistoryFooter(
                          controller: widget.historyController!,
                        ),
                      ),
                    ),
                  const SliverToBoxAdapter(child: SizedBox(height: 48)),
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}

class _ReviewHeader extends StatelessWidget {
  const _ReviewHeader({required this.previewMode});

  final bool previewMode;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: SpeakUpDesign.space16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const SpeakUpDisplayTitle(
            key: Key('review-page-title'),
            title: 'Review',
            semanticLabel: '复盘',
          ),
          if (previewMode) ...[
            const SizedBox(height: 8),
            Text('本地预览；结果不会写入正式服务。', style: SpeakUpDesign.body),
          ],
        ],
      ),
    );
  }
}

class _ReviewFilters extends StatelessWidget {
  const _ReviewFilters({required this.selected, required this.onSelected});

  final EvaluationReportSceneType? selected;
  final ValueChanged<EvaluationReportSceneType?> onSelected;

  @override
  Widget build(BuildContext context) {
    const options = <(String, EvaluationReportSceneType?)>[
      ('全部', null),
      ('面试', EvaluationReportSceneType.interview),
      ('雅思', EvaluationReportSceneType.ieltsSpeaking),
      ('日常英语', EvaluationReportSceneType.overseasDailyLife),
      ('职场英语', EvaluationReportSceneType.overseasWorkplace),
    ];
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        children: [
          for (var index = 0; index < options.length; index++) ...[
            if (index > 0) const SizedBox(width: 6),
            ChoiceChip(
              label: Text(options[index].$1),
              selected: selected == options[index].$2,
              showCheckmark: false,
              backgroundColor: SpeakUpDesign.primaryMuted,
              selectedColor: SpeakUpDesign.ink,
              side: BorderSide.none,
              shape: const StadiumBorder(),
              labelStyle: SpeakUpDesign.label.copyWith(
                color: selected == options[index].$2
                    ? Colors.white
                    : SpeakUpDesign.ink,
              ),
              onSelected: (_) => onSelected(options[index].$2),
            ),
          ],
        ],
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
    final content = _reviewCardContent(item);
    final imagePath = _reviewImage(item);
    return Semantics(
      key: primary ? const Key('review-content') : null,
      button: true,
      excludeSemantics: true,
      label:
          '${content.title}，${content.detail ?? ''}，摘要：${review.summary}，$date，$status，查看复盘详情',
      onTap: onTap,
      child: Card(
        key: Key('review-history-${review.id}'),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          key: Key('review-history-select-${review.id}'),
          onTap: onTap,
          child: Stack(
            fit: StackFit.expand,
            children: [
              Image.asset(
                imagePath,
                fit: BoxFit.cover,
                excludeFromSemantics: true,
              ),
              const DecoratedBox(
                decoration: BoxDecoration(
                  gradient: LinearGradient(
                    begin: Alignment.topCenter,
                    end: Alignment.bottomCenter,
                    colors: [Colors.transparent, Color(0xB8000000)],
                    stops: [0.38, 1],
                  ),
                ),
              ),
              Padding(
                padding: const EdgeInsets.all(12),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Spacer(),
                    Text(
                      content.title,
                      key: primary ? const Key('review-title') : null,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: SpeakUpDesign.cardTitle.copyWith(
                        color: Colors.white,
                        fontSize: 16,
                      ),
                    ),
                    const SizedBox(height: SpeakUpDesign.space4),
                    Text(
                      [content.detail, date].nonNulls.join(' · '),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: SpeakUpDesign.meta.copyWith(
                        color: Colors.white.withValues(alpha: 0.82),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

({String title, String? detail}) _reviewCardContent(ReviewHistoryItem item) {
  final report = item.report;
  if (report.sceneType == EvaluationReportSceneType.ieltsSpeaking) {
    if (report.practiceMode == 'FULL_MOCK') {
      return (title: 'IELTS 模考', detail: null);
    }
    final part = switch (report.practiceMode) {
      'PART_1' => 'Part 1',
      'PART_2' => 'Part 2',
      'PART_3' => 'Part 3',
      _ => 'IELTS',
    };
    return (title: 'IELTS 专项', detail: part);
  }
  if (report.sceneType == EvaluationReportSceneType.interview) {
    return (title: '模拟面试', detail: 'Interview');
  }
  final title = item.review.title.replaceFirst(RegExp(r'\s*·\s*证据不足$'), '');
  return (title: title, detail: 'Review');
}

class _FilteredReviewEmpty extends StatelessWidget {
  const _FilteredReviewEmpty();

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 48),
    child: Center(child: Text('暂无该类型的复盘报告', style: SpeakUpDesign.body)),
  );
}

String _reviewImage(ReviewHistoryItem item) {
  final report = item.report;
  if (report.sceneType == EvaluationReportSceneType.ieltsSpeaking) {
    return switch (report.practiceMode) {
      'FULL_MOCK' => 'assets/images/scenes/review-ielts-full-mock.webp',
      'PART_1' => 'assets/images/scenes/review-ielts-part-1.webp',
      'PART_2' => 'assets/images/scenes/review-ielts-part-2.webp',
      'PART_3' => 'assets/images/scenes/review-ielts-part-3.webp',
      _ => 'assets/images/scenes/review-ielts-full-mock.webp',
    };
  }
  final candidates = switch (report.sceneType) {
    EvaluationReportSceneType.interview => const [
      'assets/images/scenes/review-interview-room.webp',
      'assets/images/scenes/review-interview-remote.webp',
      'assets/images/scenes/review-interview-ready.webp',
    ],
    EvaluationReportSceneType.ieltsSpeaking => const <String>[],
    EvaluationReportSceneType.overseasWorkplace => const [
      'assets/images/scenes/workplace-scene.jpg',
      'assets/images/scenes/practice-workplace.webp',
      'assets/images/scenes/ielts-topic-technology-media.webp',
    ],
    EvaluationReportSceneType.overseasDailyLife => const [
      'assets/images/scenes/travel-scene.jpg',
      'assets/images/scenes/small-talk.jpg',
      'assets/images/scenes/practice-travel.webp',
    ],
  };
  final hash = item.review.id.codeUnits.fold<int>(
    0,
    (sum, value) => sum + value,
  );
  return candidates[hash % candidates.length];
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

String _compactDateLabel(DateTime value) {
  final local = value.toLocal();
  return '${local.month}月${local.day}日';
}

String _statusLabel(EvaluationReport report) =>
    report.scoreability == EvaluationReportScoreability.insufficient
    ? '证据不足'
    : '已完成';
