/// Review module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
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
  String? _selectedReviewId;

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

  @override
  Widget build(BuildContext context) {
    final controller = widget.historyController;
    final currentReview = widget.agentController?.review;
    final showCurrentReview =
        controller != null &&
        currentReview != null &&
        !controller.items.any((item) => item.review.id == currentReview.id);
    final selectableReviewIds = <String>[
      if (showCurrentReview) currentReview.id,
      if (controller != null)
        for (final item in controller.items) item.review.id,
    ];
    final selectedReviewId = selectableReviewIds.contains(_selectedReviewId)
        ? _selectedReviewId
        : selectableReviewIds.firstOrNull;
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
          child: ListView(
            key: const Key('review-history-list'),
            physics: const AlwaysScrollableScrollPhysics(),
            padding: const EdgeInsets.fromLTRB(20, 28, 20, 140),
            children: [
              const Text(
                '复盘',
                style: TextStyle(fontSize: 32, fontWeight: FontWeight.w800),
              ),
              const SizedBox(height: 8),
              Text(
                widget.previewMode
                    ? '本地 UI Mock；复盘结果不会写入正式服务。'
                    : '这里显示当前账号在服务端保存的正式复盘结果。',
                style: const TextStyle(color: Color(0xFF696B73), fontSize: 15),
              ),
              const SizedBox(height: 28),
              if (controller == null && widget.agentController?.review != null)
                _CurrentReviewContent(
                  controller: widget.agentController!,
                  selected: true,
                  onSelect: () {},
                )
              else if (controller == null)
                _EmptyReview(
                  practiceAvailable: widget.practiceAvailable,
                  previewMode: widget.previewMode,
                )
              else if (!showCurrentReview &&
                  controller.items.isEmpty &&
                  controller.isLoading)
                const _HistoryLoading()
              else if (!showCurrentReview &&
                  controller.items.isEmpty &&
                  controller.errorMessage != null)
                _HistoryFailure(
                  message: controller.errorMessage!,
                  onRetry: controller.retryLastFailure,
                )
              else if (!showCurrentReview && controller.items.isEmpty)
                _EmptyReview(
                  practiceAvailable: widget.practiceAvailable,
                  previewMode: widget.previewMode,
                )
              else ...[
                if (showCurrentReview) ...[
                  _CurrentReviewContent(
                    controller: widget.agentController!,
                    selected: selectedReviewId == currentReview.id,
                    onSelect: () {
                      setState(() => _selectedReviewId = currentReview.id);
                    },
                  ),
                  const SizedBox(height: 16),
                ],
                for (final item in controller.items) ...[
                  _ReviewHistoryContent(
                    item: item,
                    selected: selectedReviewId == item.review.id,
                    onSelect: () {
                      setState(() => _selectedReviewId = item.review.id);
                    },
                    agentController:
                        widget.agentController?.review?.id == item.review.id
                        ? widget.agentController
                        : null,
                  ),
                  const SizedBox(height: 16),
                ],
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
              if (widget.agentController?.mediaErrorMessage
                  case final message?) ...[
                const SizedBox(height: 12),
                Text(
                  message,
                  key: const Key('review-media-error-message'),
                  style: const TextStyle(color: Color(0xFF8B2E26)),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

// The current Review is a server response from the completed Practice flow.
// It stays visible while history is loading or unavailable, but is never added
// to the history controller's items or cursor.
class _CurrentReviewContent extends StatelessWidget {
  const _CurrentReviewContent({
    required this.controller,
    required this.selected,
    required this.onSelect,
  });

  final AgentController controller;
  final bool selected;
  final VoidCallback onSelect;

  @override
  Widget build(BuildContext context) {
    final review = controller.review!;
    return Card(
      key: Key('review-current-${review.id}'),
      elevation: 0,
      color: Colors.white,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
      child: InkWell(
        key: selected
            ? const Key('review-content')
            : Key('review-current-select-${review.id}'),
        borderRadius: BorderRadius.circular(20),
        onTap: onSelect,
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(
                    child: Text(
                      review.title,
                      key: selected
                          ? const Key('review-title')
                          : Key('review-current-title-${review.id}'),
                      style: const TextStyle(
                        fontSize: 21,
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  const Text(
                    '刚完成',
                    key: Key('review-current-label'),
                    style: TextStyle(color: Color(0xFF696B73), fontSize: 13),
                  ),
                  const SizedBox(width: 4),
                  Icon(
                    selected
                        ? Icons.expand_less_rounded
                        : Icons.expand_more_rounded,
                  ),
                ],
              ),
              if (selected) ...[
                const SizedBox(height: 14),
                _ReviewSection(title: '整体表现', body: review.summary),
                const Divider(height: 24),
                _ReviewSection(title: '做得好的地方', body: review.strength),
                const Divider(height: 24),
                _ReviewSection(title: '下一次重点', body: review.nextFocus),
                if (controller.recordings.isNotEmpty) ...[
                  const SizedBox(height: 16),
                  PracticeRecordingsCard(controller: controller, title: '练习录音'),
                ],
              ],
            ],
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

class _ReviewHistoryContent extends StatelessWidget {
  const _ReviewHistoryContent({
    required this.item,
    required this.selected,
    required this.onSelect,
    this.agentController,
  });

  final ReviewHistoryItem item;
  final bool selected;
  final VoidCallback onSelect;
  final AgentController? agentController;

  @override
  Widget build(BuildContext context) {
    final review = item.review;
    return Card(
      key: Key('review-history-${review.id}'),
      elevation: 0,
      color: Colors.white,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
      child: InkWell(
        key: selected
            ? const Key('review-content')
            : Key('review-history-select-${review.id}'),
        borderRadius: BorderRadius.circular(20),
        onTap: onSelect,
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(
                    child: Text(
                      review.title,
                      key: selected
                          ? const Key('review-title')
                          : Key('review-history-title-${review.id}'),
                      style: const TextStyle(
                        fontSize: 21,
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Text(
                    _dateLabel(item.completedAt),
                    style: const TextStyle(
                      color: Color(0xFF777983),
                      fontSize: 13,
                    ),
                  ),
                  const SizedBox(width: 4),
                  Icon(
                    selected
                        ? Icons.expand_less_rounded
                        : Icons.expand_more_rounded,
                  ),
                ],
              ),
              if (selected) ...[
                const SizedBox(height: 14),
                _ReviewSection(title: '整体表现', body: review.summary),
                const Divider(height: 24),
                _ReviewSection(title: '做得好的地方', body: review.strength),
                const Divider(height: 24),
                _ReviewSection(title: '下一次重点', body: review.nextFocus),
                if (agentController case final controller?
                    when controller.recordings.isNotEmpty) ...[
                  const SizedBox(height: 16),
                  PracticeRecordingsCard(controller: controller, title: '练习录音'),
                ],
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _ReviewSection extends StatelessWidget {
  const _ReviewSection({required this.title, required this.body});

  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(title, style: const TextStyle(fontWeight: FontWeight.w700)),
        const SizedBox(height: 6),
        Text(
          body,
          style: const TextStyle(color: Color(0xFF595B63), height: 1.45),
        ),
      ],
    );
  }
}

String _dateLabel(DateTime value) {
  final local = value.toLocal();
  String twoDigits(int number) => number.toString().padLeft(2, '0');
  return '${local.year}-${twoDigits(local.month)}-${twoDigits(local.day)}';
}
