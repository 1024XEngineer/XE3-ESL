/// Review reads and presents canonical Evaluation reports.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presentation.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_practice_report.dart';
import 'package:speakup/features/coaching/review/ielts_practice_report_decoder.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_decoder.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_view.dart';
import 'package:speakup/features/coaching/review/practice_report_status.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';

const _ieltsSpeakingReportSchema = 'ielts-speaking-report/v1';
const _ieltsSpeakingPracticeReportSchema = 'ielts-speaking-practice-report/v1';
const _legacyGeneralSceneReportSchema = 'general-scene-evaluation/v1';

class ReviewPage extends StatefulWidget {
  const ReviewPage({
    this.showBackButton = false,
    this.onExit,
    this.previewMode = false,
    this.practiceAvailable = true,
    this.historyController,
    this.ieltsSpeakingReportController,
    this.autoload = true,
    super.key,
  });

  final bool showBackButton;
  final VoidCallback? onExit;
  final bool previewMode;
  final bool practiceAvailable;
  final ReviewHistoryController? historyController;
  final IeltsSpeakingReportController? ieltsSpeakingReportController;
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
    if (_isFullMockIeltsReport(item.report)) {
      _openIeltsReport(item);
      return;
    }
    unawaited(
      Navigator.of(context).push<void>(
        MaterialPageRoute<void>(
          builder: (_) => ReviewReportDetailPage(item: item),
        ),
      ),
    );
  }

  void _openIeltsReport(ReviewHistoryItem item) {
    try {
      final detail = decodeIeltsSpeakingReportDetail(item.report.detail);
      unawaited(
        Navigator.of(context).push<void>(
          MaterialPageRoute<void>(
            builder: (_) => _IeltsReportDetailPage(report: detail),
          ),
        ),
      );
    } on IeltsSpeakingReportDecodeException {
      unawaited(
        Navigator.of(context).push<void>(
          MaterialPageRoute<void>(
            builder: (_) => ReviewReportDetailPage(item: item),
          ),
        ),
      );
    }
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
    report.practiceMode == 'FULL_MOCK' &&
    report.detailSchema == _ieltsSpeakingReportSchema;

bool _isIeltsSectionReport(EvaluationReport report) =>
    report.sceneType == EvaluationReportSceneType.ieltsSpeaking &&
    (report.practiceMode == 'PART_1' ||
        report.practiceMode == 'PART_2' ||
        report.practiceMode == 'PART_3') &&
    (report.detailSchema == _ieltsSpeakingPracticeReportSchema ||
        report.detailSchema == _legacyGeneralSceneReportSchema);

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
      return const IeltsSpeakingAbilityProfile(report: null, loading: false);
    }
    return AnimatedBuilder(
      animation: controller,
      builder: (context, _) {
        final item = _latestFullMock(controller.items);
        if (item == null) {
          return IeltsSpeakingAbilityProfile(
            report: null,
            loading: controller.isLoading,
          );
        }
        try {
          return IeltsSpeakingAbilityProfile(
            report: decodeIeltsSpeakingReportDetail(item.report.detail),
            loading: false,
            completedAt: item.completedAt,
          );
        } on IeltsSpeakingReportDecodeException {
          return const IeltsSpeakingAbilityProfile(
            report: null,
            loading: false,
          );
        }
      },
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

class _IeltsReportDetailPage extends StatelessWidget {
  const _IeltsReportDetailPage({required this.report});

  final IeltsSpeakingReport report;

  @override
  Widget build(BuildContext context) {
    return IeltsSpeakingReportScaffold(
      key: const Key('ielts-speaking-report-detail-page'),
      title: 'IELTS 口语模考报告',
      child: IeltsSpeakingReadyReportView(report: report),
    );
  }
}

class ReviewReportDetailPage extends StatelessWidget {
  const ReviewReportDetailPage({required this.item, super.key});

  final ReviewHistoryItem item;

  @override
  Widget build(BuildContext context) {
    final report = item.report;
    final sectionDetail = _decodeSectionDetail(report);
    final isIeltsSectionReport = _isIeltsSectionReport(report);
    final expectsSectionDetail =
        report.sceneType == EvaluationReportSceneType.ieltsSpeaking &&
        report.detailSchema == _ieltsSpeakingPracticeReportSchema;
    final findings = report.dimensions
        .expand((dimension) => dimension.improvements)
        .toList(growable: false);
    final priorityFeedback =
        report.scoreability == EvaluationReportScoreability.insufficient
        ? null
        : _ieltsPriorityFeedback(report, sectionDetail);
    return Scaffold(
      key: const Key('review-detail-page'),
      appBar: AppBar(
        title: Text(
          isIeltsSectionReport ? evaluationReportTitle(report) : '复盘详情',
          key: isIeltsSectionReport ? const Key('review-detail-title') : null,
        ),
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
            if (!isIeltsSectionReport) ...[_ReviewDetailHeader(item: item)],
            if (!isIeltsSectionReport &&
                report.scoreability ==
                    EvaluationReportScoreability.insufficient) ...[
              const SizedBox(height: 12),
              const _ReviewStatusNotice(),
            ],
            if (isIeltsSectionReport) ...[
              const SizedBox(height: 12),
              _IeltsSectionPerformance(report: report),
            ],
            if (isIeltsSectionReport && priorityFeedback != null) ...[
              const SizedBox(height: 12),
              _IeltsPriorityFocus(feedback: priorityFeedback),
            ],
            if (sectionDetail != null) ...[
              const SizedBox(height: 12),
              _IeltsSectionReport(
                report: report,
                detail: sectionDetail,
                excludedFindingId: priorityFeedback?.finding.id,
              ),
            ] else if (expectsSectionDetail) ...[
              const SizedBox(height: 12),
              const _ReviewDetailSection(
                key: Key('ielts-section-detail-invalid'),
                title: '分段复盘暂不可用',
                body: '专项报告的逐题数据无法识别，请稍后重试。',
              ),
            ] else if (isIeltsSectionReport && findings.isNotEmpty) ...[
              const SizedBox(height: 12),
              _IeltsDetailedFeedback(findings: findings),
            ],
            if (!isIeltsSectionReport && report.dimensions.isNotEmpty) ...[
              const SizedBox(height: 12),
              _ReviewDimensions(dimensions: report.dimensions),
            ],
            if (!isIeltsSectionReport && findings.isNotEmpty) ...[
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
    final summary = _visibleReviewSummary(item.report.summary);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: SpeakUpDesign.space8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            item.review.title,
            key: const Key('review-detail-title'),
            style: SpeakUpDesign.sectionTitle.copyWith(fontSize: 24),
          ),
          const SizedBox(height: SpeakUpDesign.space4),
          Text(
            '${_detailDateLabel(item.completedAt)} · ${_statusLabel(item.report)}',
            style: SpeakUpDesign.meta,
          ),
          if (summary != null) ...[
            const SizedBox(height: SpeakUpDesign.space12),
            Text(
              summary,
              key: const Key('review-detail-summary'),
              style: SpeakUpDesign.body,
            ),
          ],
        ],
      ),
    );
  }
}

String? _visibleReviewSummary(String summary) =>
    const <String>{
      '本次练习已形成场景沟通评估，可按优先行动继续复练。',
      '本次练习已形成面试表达评估，可按优先行动继续复练。',
      '本次练习已形成 IELTS 口语评估，可按优先行动继续复练。',
      '本次练习已形成面试表达评估。',
      '本次回答已经形成可复盘的文本反馈。',
      '当前回答不足以形成可靠结论。',
      '本次练习的有效证据不足，暂不形成能力结论。',
    }.contains(summary)
    ? null
    : summary;

class _IeltsSectionPerformance extends StatelessWidget {
  const _IeltsSectionPerformance({required this.report});

  final EvaluationReport report;

  @override
  Widget build(BuildContext context) {
    final dimensions = report.dimensions;
    final usesIeltsBandScale =
        dimensions.isNotEmpty &&
        dimensions.every(
          (dimension) =>
              dimension.scale == EvaluationReportScoreScale.ieltsBand,
        );
    final showsRadar =
        dimensions.isNotEmpty &&
        report.scoreability != EvaluationReportScoreability.insufficient;
    final byKey = {
      for (final dimension in dimensions) dimension.key: dimension,
    };
    final ordered = usesIeltsBandScale
        ? <EvaluationReportDimension?>[
            byKey['IELTS_FC'],
            byKey['IELTS_PR'],
            byKey['IELTS_GRA'],
            byKey['IELTS_LR'],
          ]
        : <EvaluationReportDimension?>[
            byKey['TASK_ACHIEVEMENT'],
            byKey['CLARITY_COHERENCE'],
            byKey['LANGUAGE_CONTROL'],
            byKey['INTERACTION'],
          ];
    final labels = usesIeltsBandScale
        ? const ['流利与连贯', '发音', '语法', '词汇']
        : const ['任务达成', '清晰与连贯', '语言运用', '互动表现'];
    final status =
        report.scoreability == EvaluationReportScoreability.insufficient
        ? '本次录音不足以判断，暂不形成 Band 结论。'
        : usesIeltsBandScale
        ? '本次回答已达到可评分条件；以上 Band 仅为本次表现估分。'
        : '本次回答已达到可评分条件；本报告仅反映本次表现。';
    return Card(
      key: const Key('review-detail-dimensions'),
      child: Padding(
        padding: const EdgeInsets.all(SpeakUpDesign.space20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('本次表现', style: SpeakUpDesign.sectionTitle),
            if (showsRadar) ...[
              const SizedBox(height: SpeakUpDesign.space16),
              FourAxisScoreRadar(
                axes: <FourAxisRadarAxis>[
                  FourAxisRadarAxis(label: labels[0], value: ordered[0]?.score),
                  FourAxisRadarAxis(label: labels[1], value: ordered[1]?.score),
                  FourAxisRadarAxis(label: labels[2], value: ordered[2]?.score),
                  FourAxisRadarAxis(label: labels[3], value: ordered[3]?.score),
                ],
                maximum: usesIeltsBandScale ? 9 : 100,
                semanticsKey: const Key('review-section-score-radar'),
                semanticsPrefix: '专项练习四维雷达图',
              ),
            ],
            const SizedBox(height: SpeakUpDesign.space16),
            Text(status, style: SpeakUpDesign.body),
          ],
        ),
      ),
    );
  }
}

typedef _IeltsPriorityFeedback = ({
  String dimensionKey,
  EvaluationReportFinding finding,
  EvaluationReportEvidence evidence,
});

_IeltsPriorityFeedback? _ieltsPriorityFeedback(
  EvaluationReport report,
  IeltsPracticeReportDetail? sectionDetail,
) {
  final needsTrustedEvidence =
      report.detailSchema == _ieltsSpeakingPracticeReportSchema;
  final questionByTurnId = <String, IeltsPracticeReportQuestion>{};
  for (final question in sectionDetail?.questions ?? const []) {
    final turnId = question.responseTurnId;
    if (turnId != null) questionByTurnId[turnId] = question;
  }
  for (final action in report.priorityActions) {
    for (final dimension in report.dimensions) {
      if (dimension.key != action.dimensionKey) continue;
      for (final finding in <EvaluationReportFinding>[
        ...dimension.improvements,
        ...dimension.recommendedExamples,
      ]) {
        if (finding.id == action.findingId) {
          final evidence = finding.evidence.where((item) {
            if (!needsTrustedEvidence) return true;
            final question = questionByTurnId[item.turnId];
            final transcript = question?.confirmedTranscript;
            return _englishWordCount(item.originalExcerpt) >= 1 &&
                transcript != null &&
                _englishWordCount(transcript) >= 3 &&
                question!.evidenceRefIds.contains(item.evidenceRefId);
          }).firstOrNull;
          if (evidence == null) continue;
          return (
            dimensionKey: dimension.key,
            finding: finding,
            evidence: evidence,
          );
        }
      }
    }
  }
  return null;
}

class _IeltsPriorityFocus extends StatelessWidget {
  const _IeltsPriorityFocus({required this.feedback});

  final _IeltsPriorityFeedback feedback;

  @override
  Widget build(BuildContext context) {
    final finding = feedback.finding;
    final suggestion = _userFacingIeltsSuggestion(finding.suggestion);
    final evidence = feedback.evidence.originalExcerpt;
    return Container(
      key: const Key('review-detail-priority-focus'),
      padding: const EdgeInsets.all(SpeakUpDesign.space20),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surfaceMuted,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('最该先改的一点', style: SpeakUpDesign.cardTitle),
          const SizedBox(height: SpeakUpDesign.space16),
          Text(
            _dimensionLabel(feedback.dimensionKey),
            style: SpeakUpDesign.label.copyWith(color: SpeakUpDesign.secondary),
          ),
          const SizedBox(height: SpeakUpDesign.space16),
          Text('报告依据的原句', style: SpeakUpDesign.label),
          const SizedBox(height: SpeakUpDesign.space4),
          Text('“$evidence”', style: SpeakUpDesign.body),
          const SizedBox(height: SpeakUpDesign.space16),
          Text('为什么要先改', style: SpeakUpDesign.label),
          const SizedBox(height: SpeakUpDesign.space4),
          Text(
            finding.message,
            style: SpeakUpDesign.body.copyWith(color: SpeakUpDesign.ink),
          ),
          if (suggestion != null && suggestion != finding.message) ...[
            const Divider(height: SpeakUpDesign.space32),
            Text('下一步练习', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: SpeakUpDesign.space16),
            Text(suggestion, style: SpeakUpDesign.body),
          ],
        ],
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

IeltsPracticeReportDetail? _decodeSectionDetail(EvaluationReport report) {
  if (report.sceneType != EvaluationReportSceneType.ieltsSpeaking ||
      report.detailSchema != _ieltsSpeakingPracticeReportSchema) {
    return null;
  }
  try {
    final detail = decodeIeltsPracticeReportDetail(report.detail);
    if (!_sectionDetailMatchesPracticeMode(report.practiceMode, detail)) {
      throw const IeltsPracticeReportDecodeException();
    }
    final strengthIds = <String>{
      for (final dimension in report.dimensions)
        for (final finding in dimension.strengths) finding.id,
    };
    final improvementIds = <String>{
      for (final dimension in report.dimensions)
        for (final finding in dimension.improvements) finding.id,
    };
    final exampleIds = <String>{
      for (final dimension in report.dimensions)
        for (final finding in dimension.recommendedExamples) finding.id,
    };
    for (final section in detail.sectionReviews) {
      if (!strengthIds.containsAll(section.strengthFindingIds) ||
          !improvementIds.containsAll(section.improvementFindingIds) ||
          !exampleIds.containsAll(section.upgradeExampleFindingIds)) {
        throw const IeltsPracticeReportDecodeException();
      }
    }
    return detail;
  } on IeltsPracticeReportDecodeException {
    return null;
  }
}

bool _sectionDetailMatchesPracticeMode(
  String practiceMode,
  IeltsPracticeReportDetail detail,
) => switch (practiceMode) {
  'PART_1' =>
    detail.reportScope == PracticeReportScope.part1 &&
        _matchesParts(detail.availableSections, const <IeltsSpeakingPartId>[
          IeltsSpeakingPartId.part1,
        ]),
  'PART_2' =>
    detail.reportScope == PracticeReportScope.part2And3 &&
        _matchesParts(detail.availableSections, const <IeltsSpeakingPartId>[
          IeltsSpeakingPartId.part2,
          IeltsSpeakingPartId.part3,
        ]),
  'PART_3' =>
    detail.reportScope == PracticeReportScope.part3 &&
        _matchesParts(detail.availableSections, const <IeltsSpeakingPartId>[
          IeltsSpeakingPartId.part3,
        ]),
  _ => false,
};

bool _matchesParts(
  List<IeltsSpeakingPartId> actual,
  List<IeltsSpeakingPartId> expected,
) {
  if (actual.length != expected.length) return false;
  for (var index = 0; index < actual.length; index++) {
    if (actual[index] != expected[index]) return false;
  }
  return true;
}

class _IeltsSectionReport extends StatelessWidget {
  const _IeltsSectionReport({
    required this.report,
    required this.detail,
    required this.excludedFindingId,
  });

  final EvaluationReport report;
  final IeltsPracticeReportDetail detail;
  final String? excludedFindingId;

  @override
  Widget build(BuildContext context) {
    final strengths = <String, _IeltsFindingFeedback>{
      for (final dimension in report.dimensions)
        for (final finding in dimension.strengths)
          finding.id: (dimensionKey: dimension.key, finding: finding),
    };
    final improvements = <String, _IeltsFindingFeedback>{
      for (final dimension in report.dimensions)
        for (final finding in dimension.improvements)
          finding.id: (dimensionKey: dimension.key, finding: finding),
    };
    final answered = detail.questions
        .where((question) => question.confirmedTranscript != null)
        .length;
    return Card(
      key: const Key('ielts-section-report'),
      clipBehavior: Clip.antiAlias,
      child: ExpansionTile(
        tilePadding: const EdgeInsets.symmetric(
          horizontal: SpeakUpDesign.space20,
          vertical: SpeakUpDesign.space4,
        ),
        childrenPadding: const EdgeInsets.fromLTRB(
          SpeakUpDesign.space20,
          0,
          SpeakUpDesign.space20,
          SpeakUpDesign.space20,
        ),
        title: Text('逐题反馈', style: SpeakUpDesign.cardTitle),
        subtitle: Text(
          '$answered/${detail.questions.length} 题已回答 · 点开查看原句与建议',
          style: SpeakUpDesign.meta,
        ),
        children: [
          for (
            var index = 0;
            index < detail.sectionReviews.length;
            index++
          ) ...[
            if (index > 0) const Divider(height: 33),
            _IeltsSectionCard(
              section: detail.sectionReviews[index],
              questions: detail.questions
                  .where(
                    (question) =>
                        question.partId == detail.sectionReviews[index].partId,
                  )
                  .toList(growable: false),
              strengths: strengths,
              improvements: improvements,
              excludedFindingId: excludedFindingId,
            ),
          ],
        ],
      ),
    );
  }
}

class _IeltsDetailedFeedback extends StatelessWidget {
  const _IeltsDetailedFeedback({required this.findings});

  final List<EvaluationReportFinding> findings;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: const Key('review-detail-feedback'),
      clipBehavior: Clip.antiAlias,
      child: ExpansionTile(
        tilePadding: const EdgeInsets.symmetric(
          horizontal: SpeakUpDesign.space20,
          vertical: SpeakUpDesign.space4,
        ),
        childrenPadding: const EdgeInsets.fromLTRB(
          SpeakUpDesign.space20,
          0,
          SpeakUpDesign.space20,
          SpeakUpDesign.space20,
        ),
        title: Text('查看全部评分依据', style: SpeakUpDesign.cardTitle),
        subtitle: Text(
          '查看其余 ${findings.length} 条分项反馈',
          style: SpeakUpDesign.meta,
        ),
        children: [
          for (var index = 0; index < findings.length; index++) ...[
            if (index > 0) const Divider(height: SpeakUpDesign.space24),
            Column(
              key: Key('review-feedback-${findings[index].id}'),
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(findings[index].message, style: SpeakUpDesign.body),
                if (_userFacingIeltsSuggestion(findings[index].suggestion)
                    case final suggestion?) ...[
                  if (suggestion != findings[index].message) ...[
                    const SizedBox(height: SpeakUpDesign.space8),
                    Text('练习方法：$suggestion', style: SpeakUpDesign.body),
                  ],
                ],
              ],
            ),
          ],
        ],
      ),
    );
  }
}

class _IeltsSectionCard extends StatelessWidget {
  const _IeltsSectionCard({
    required this.section,
    required this.questions,
    required this.strengths,
    required this.improvements,
    required this.excludedFindingId,
  });

  final IeltsPracticeSectionReview section;
  final List<IeltsPracticeReportQuestion> questions;
  final Map<String, _IeltsFindingFeedback> strengths;
  final Map<String, _IeltsFindingFeedback> improvements;
  final String? excludedFindingId;

  @override
  Widget build(BuildContext context) {
    final answered = questions
        .where((question) => question.confirmedTranscript != null)
        .length;
    return Padding(
      key: Key('ielts-section-${section.partId.name}'),
      padding: const EdgeInsets.only(top: 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                _ieltsPartLabel(section.partId),
                style: SpeakUpDesign.cardTitle,
              ),
              const Spacer(),
              Text(
                '$answered/${questions.length} 题已回答',
                style: SpeakUpDesign.meta,
              ),
            ],
          ),
          for (final question in questions) ...[
            const Divider(height: 1),
            _IeltsQuestionFeedbackTile(
              question: question,
              strengths: _questionFindings(
                question,
                section.strengthFindingIds,
                strengths,
                excludedFindingId: excludedFindingId,
              ),
              improvements: _questionFindings(
                question,
                section.improvementFindingIds,
                improvements,
                excludedFindingId: excludedFindingId,
              ),
            ),
          ],
        ],
      ),
    );
  }
}

typedef _IeltsFindingFeedback = ({
  String dimensionKey,
  EvaluationReportFinding finding,
});

typedef _IeltsQuestionFinding = ({
  String dimensionKey,
  EvaluationReportFinding finding,
  EvaluationReportEvidence evidence,
});

List<_IeltsQuestionFinding> _questionFindings(
  IeltsPracticeReportQuestion question,
  List<String> findingIds,
  Map<String, _IeltsFindingFeedback> findings, {
  String? excludedFindingId,
}) {
  final transcript = question.confirmedTranscript;
  final turnId = question.responseTurnId;
  if (transcript == null ||
      turnId == null ||
      _englishWordCount(transcript) < 3) {
    return const [];
  }
  final matches = <_IeltsQuestionFinding>[];
  for (final id in findingIds) {
    if (id == excludedFindingId) continue;
    final feedback = findings[id];
    if (feedback == null) continue;
    final evidence = feedback.finding.evidence.where((item) {
      return item.turnId == turnId &&
          question.evidenceRefIds.contains(item.evidenceRefId) &&
          _englishWordCount(item.originalExcerpt) >= 1;
    }).firstOrNull;
    if (evidence != null) {
      matches.add((
        dimensionKey: feedback.dimensionKey,
        finding: feedback.finding,
        evidence: evidence,
      ));
    }
  }
  return matches;
}

class _IeltsQuestionFeedbackTile extends StatelessWidget {
  const _IeltsQuestionFeedbackTile({
    required this.question,
    required this.strengths,
    required this.improvements,
  });

  final IeltsPracticeReportQuestion question;
  final List<_IeltsQuestionFinding> strengths;
  final List<_IeltsQuestionFinding> improvements;

  @override
  Widget build(BuildContext context) {
    final transcript = question.confirmedTranscript;
    return ExpansionTile(
      key: Key('ielts-question-feedback-${question.questionId}'),
      tilePadding: EdgeInsets.zero,
      childrenPadding: const EdgeInsets.only(bottom: SpeakUpDesign.space16),
      title: Text(
        'Q${question.index}. ${question.questionText}',
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
        style: SpeakUpDesign.body.copyWith(
          color: SpeakUpDesign.ink,
          fontWeight: FontWeight.w600,
        ),
      ),
      subtitle: transcript == null
          ? Text('未回答', style: SpeakUpDesign.meta)
          : Text(
              '我的回答：$transcript',
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: SpeakUpDesign.meta,
            ),
      children: [
        for (var index = 0; index < strengths.length; index++) ...[
          if (index > 0) const SizedBox(height: SpeakUpDesign.space16),
          _IeltsQuestionFindingBlock(
            title: '做得好',
            feedback: strengths[index],
            transcript: transcript,
          ),
        ],
        if (strengths.isNotEmpty && improvements.isNotEmpty)
          const SizedBox(height: SpeakUpDesign.space16),
        for (var index = 0; index < improvements.length; index++) ...[
          if (index > 0) const SizedBox(height: SpeakUpDesign.space16),
          _IeltsQuestionFindingBlock(
            title: '待改进',
            feedback: improvements[index],
            transcript: transcript,
            showSuggestion: true,
          ),
        ],
        if (strengths.isEmpty && improvements.isEmpty)
          Align(
            alignment: Alignment.centerLeft,
            child: Text('这道题暂无单独反馈。', style: SpeakUpDesign.meta),
          ),
      ],
    );
  }
}

class _IeltsQuestionFindingBlock extends StatelessWidget {
  const _IeltsQuestionFindingBlock({
    required this.title,
    required this.feedback,
    required this.transcript,
    this.showSuggestion = false,
  });

  final String title;
  final _IeltsQuestionFinding feedback;
  final String? transcript;
  final bool showSuggestion;

  @override
  Widget build(BuildContext context) {
    final suggestion = showSuggestion
        ? _userFacingIeltsSuggestion(feedback.finding.suggestion)
        : null;
    final excerpt = feedback.evidence.originalExcerpt;
    final showExcerpt =
        showSuggestion &&
        _normalizedText(excerpt) != _normalizedText(transcript);
    return Align(
      alignment: Alignment.centerLeft,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '$title · ${_dimensionLabel(feedback.dimensionKey)}',
            style: SpeakUpDesign.label.copyWith(color: SpeakUpDesign.secondary),
          ),
          if (showExcerpt) ...[
            const SizedBox(height: SpeakUpDesign.space4),
            Text('“$excerpt”', style: SpeakUpDesign.body),
          ],
          const SizedBox(height: SpeakUpDesign.space4),
          Text(feedback.finding.message, style: SpeakUpDesign.body),
          if (suggestion != null && suggestion != feedback.finding.message) ...[
            const SizedBox(height: SpeakUpDesign.space8),
            Text('怎么练：$suggestion', style: SpeakUpDesign.meta),
          ],
        ],
      ),
    );
  }
}

String _ieltsPartLabel(IeltsSpeakingPartId partId) => switch (partId) {
  IeltsSpeakingPartId.part1 => 'Part 1',
  IeltsSpeakingPartId.part2 => 'Part 2',
  IeltsSpeakingPartId.part3 => 'Part 3',
};

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
    final showsRadar =
        dimensions.length == 4 &&
        dimensions.every((dimension) => dimension.score != null) &&
        dimensions.every(
          (dimension) => dimension.scale == dimensions.first.scale,
        );
    return Card(
      key: const Key('review-detail-dimensions'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('分项表现', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 14),
            if (showsRadar) ...[
              FourAxisScoreRadar(
                axes: [
                  for (final dimension in dimensions)
                    FourAxisRadarAxis(
                      label: _dimensionLabel(dimension.key),
                      value: dimension.score,
                    ),
                ],
                maximum:
                    dimensions.first.scale ==
                        EvaluationReportScoreScale.ieltsBand
                    ? 9
                    : 100,
                semanticsKey: const Key('review-generic-score-radar'),
                semanticsPrefix: '通用评估四维雷达图',
              ),
            ],
            for (var index = 0; index < dimensions.length; index++) ...[
              if (showsRadar || index > 0) ...[
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
    final ordered = <EvaluationReportFinding>[
      ...findings.where((finding) => priorityIds.contains(finding.id)),
      ...findings.where((finding) => !priorityIds.contains(finding.id)),
    ];
    final primary = ordered.first;
    final remaining = ordered.skip(1).toList(growable: false);
    return Card(
      key: const Key('review-detail-feedback'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('下一步先练', style: SpeakUpDesign.cardTitle),
            const SizedBox(height: 14),
            Column(
              key: Key('review-feedback-${primary.id}'),
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  primary.message,
                  style: SpeakUpDesign.body.copyWith(color: SpeakUpDesign.ink),
                ),
                if (primary.suggestion case final suggestion?) ...[
                  if (suggestion != primary.message) ...[
                    const SizedBox(height: 6),
                    Text('建议：$suggestion', style: SpeakUpDesign.body),
                  ],
                ],
              ],
            ),
            if (remaining.isNotEmpty) ...[
              const SizedBox(height: SpeakUpDesign.space8),
              Theme(
                data: Theme.of(
                  context,
                ).copyWith(dividerColor: Colors.transparent),
                child: ExpansionTile(
                  key: const Key('review-feedback-more'),
                  tilePadding: EdgeInsets.zero,
                  childrenPadding: EdgeInsets.zero,
                  dense: true,
                  title: Text(
                    '查看其余 ${remaining.length} 条建议',
                    style: SpeakUpDesign.label,
                  ),
                  children: [
                    for (final finding in remaining)
                      Padding(
                        key: Key('review-feedback-${finding.id}'),
                        padding: const EdgeInsets.only(
                          bottom: SpeakUpDesign.space12,
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(finding.message, style: SpeakUpDesign.body),
                            if (finding.suggestion case final suggestion?) ...[
                              if (suggestion != finding.message) ...[
                                const SizedBox(height: 6),
                                Text(
                                  '建议：$suggestion',
                                  style: SpeakUpDesign.body,
                                ),
                              ],
                            ],
                          ],
                        ),
                      ),
                  ],
                ),
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
  final formatted = _scoreValueLabel(value);
  return scale == EvaluationReportScoreScale.ieltsBand
      ? '$formatted / 9'
      : '$formatted / 100';
}

String _scoreValueLabel(double value) => value == value.roundToDouble()
    ? value.toInt().toString()
    : value.toStringAsFixed(1);

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
        'TASK_ACHIEVEMENT': '任务达成',
        'CLARITY_COHERENCE': '清晰度与连贯性',
        'LANGUAGE_CONTROL': '语言运用',
        'INTERACTION': '互动表现',
        'IELTS_FC': '流利与连贯',
        'IELTS_LR': '词汇资源',
        'IELTS_GRA': '语法范围与准确性',
        'IELTS_PR': '发音',
      }[key] ??
      '未识别评分维度';
}

int _englishWordCount(String value) =>
    RegExp(r"[A-Za-z]+(?:['’-][A-Za-z]+)*").allMatches(value).length;

String _normalizedText(String? value) =>
    (value ?? '').trim().toLowerCase().replaceAll(RegExp(r'\s+'), ' ');

String? _userFacingIeltsSuggestion(String? value) {
  if (value == null) return null;
  const providerDetailMarker = '结合本次原句，还可以这样调整：';
  final markerIndex = value.indexOf(providerDetailMarker);
  final visible = (markerIndex < 0 ? value : value.substring(0, markerIndex))
      .trim();
  return visible.isEmpty ? null : visible;
}

String _compactDateLabel(DateTime value) {
  final local = value.toLocal();
  return '${local.month}月${local.day}日';
}

String _detailDateLabel(DateTime value) {
  final local = value.toLocal();
  String twoDigits(int number) => number.toString().padLeft(2, '0');
  return '${local.year}年${local.month}月${local.day}日 '
      '${twoDigits(local.hour)}:${twoDigits(local.minute)}';
}
