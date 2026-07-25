/// Review module boundary.
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/practice/practice_recordings.dart';

class ReviewPage extends StatefulWidget {
  const ReviewPage({
    this.showBackButton = false,
    this.previewMode = false,
    this.agentController,
    super.key,
  });

  final bool showBackButton;
  final bool previewMode;
  final AgentController? agentController;

  @override
  State<ReviewPage> createState() => _ReviewPageState();
}

class _ReviewPageState extends State<ReviewPage> {
  @override
  void initState() {
    super.initState();
    widget.agentController?.addListener(_rebuild);
  }

  @override
  void didUpdateWidget(covariant ReviewPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.agentController == widget.agentController) {
      return;
    }
    oldWidget.agentController?.removeListener(_rebuild);
    widget.agentController?.addListener(_rebuild);
  }

  @override
  void dispose() {
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
    final review = widget.agentController?.review;
    final practiceAvailable =
        widget.agentController?.supportsPracticeFlow ?? true;
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
        child: ListView(
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
                  : '练习记录、证据反馈和下一步建议集中在这里。',
              style: const TextStyle(color: Color(0xFF696B73), fontSize: 15),
            ),
            const SizedBox(height: 28),
            if (review == null)
              _EmptyReview(practiceAvailable: practiceAvailable)
            else
              _ReviewContent(
                review: review,
                controller: widget.agentController,
              ),
            if (widget.agentController?.mediaErrorMessage
                case final message?) ...[
              const SizedBox(height: 14),
              Text(
                message,
                key: const Key('review-media-error-message'),
                style: const TextStyle(color: Color(0xFF8B2E26)),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _EmptyReview extends StatelessWidget {
  const _EmptyReview({required this.practiceAvailable});

  final bool practiceAvailable;

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
              practiceAvailable
                  ? '达到服务端设定的有效轮数后会自动生成一次复盘。'
                  : '待服务端场景、语音与复盘契约开放后再接入，不会展示本地模拟结果。',
              textAlign: TextAlign.center,
              style: const TextStyle(color: Color(0xFF777983)),
            ),
          ],
        ),
      ),
    );
  }
}

class _ReviewContent extends StatelessWidget {
  const _ReviewContent({required this.review, required this.controller});

  final AgentReview review;
  final AgentController? controller;

  @override
  Widget build(BuildContext context) {
    return Column(
      key: const Key('review-content'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          review.title,
          key: const Key('review-title'),
          style: const TextStyle(fontSize: 21, fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 14),
        _ReviewCard(
          title: '整体表现',
          body: review.summary,
          icon: Icons.subject_rounded,
        ),
        const SizedBox(height: 12),
        _ReviewCard(
          title: '做得好的地方',
          body: review.strength,
          icon: Icons.check_circle_outline_rounded,
        ),
        const SizedBox(height: 12),
        _ReviewCard(
          title: '下一次重点',
          body: review.nextFocus,
          icon: Icons.track_changes_rounded,
        ),
        if (controller case final value? when value.recordings.isNotEmpty) ...[
          const SizedBox(height: 12),
          PracticeRecordingsCard(controller: value, title: '练习录音'),
        ],
      ],
    );
  }
}

class _ReviewCard extends StatelessWidget {
  const _ReviewCard({
    required this.title,
    required this.body,
    required this.icon,
  });

  final String title;
  final String body;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    return Card(
      elevation: 0,
      color: Colors.white,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(icon, color: const Color(0xFF505158)),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: const TextStyle(fontWeight: FontWeight.w700),
                  ),
                  const SizedBox(height: 6),
                  Text(
                    body,
                    style: const TextStyle(
                      color: Color(0xFF595B63),
                      height: 1.45,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
