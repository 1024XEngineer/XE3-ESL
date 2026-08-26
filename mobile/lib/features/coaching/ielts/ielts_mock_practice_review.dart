part of 'ielts_mock_practice.dart';

class _SectionReviewPage extends StatefulWidget {
  const _SectionReviewPage({
    required this.controller,
    required this.answerCount,
    required this.title,
  });

  final SessionEvaluationController? controller;
  final int answerCount;
  final String title;

  @override
  State<_SectionReviewPage> createState() => _SectionReviewPageState();
}

class _SectionReviewPageState extends State<_SectionReviewPage> {
  EvaluationReport? _report;

  @override
  void initState() {
    super.initState();
    widget.controller?.addListener(_handleStatusChanged);
    scheduleMicrotask(_syncReadyReport);
  }

  @override
  void didUpdateWidget(covariant _SectionReviewPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller == widget.controller) return;
    oldWidget.controller?.removeListener(_handleStatusChanged);
    widget.controller?.addListener(_handleStatusChanged);
    _report = null;
    scheduleMicrotask(_syncReadyReport);
  }

  @override
  void dispose() {
    widget.controller?.removeListener(_handleStatusChanged);
    super.dispose();
  }

  void _handleStatusChanged() {
    if (!mounted) return;
    setState(() {});
    _syncReadyReport();
  }

  void _syncReadyReport() {
    final controller = widget.controller;
    if (!mounted || controller == null || _report != null) {
      return;
    }
    final report = controller.evaluation?.report;
    if (report == null) return;
    setState(() {
      _report = report;
    });
  }

  @override
  Widget build(BuildContext context) {
    final report = _report;
    if (report != null) {
      return ReviewReportDetailPage(
        item: ReviewHistoryItem(
          review: presentEvaluationReport(report),
          report: report,
          practiceSessionId: report.practiceSessionId,
          createdAt: report.createdAt,
          completedAt: report.createdAt,
        ),
      );
    }
    final controller = widget.controller;
    final status = controller?.evaluation?.status;
    final failed =
        status == SessionEvaluationStatus.failed ||
        controller?.errorMessage != null && controller?.isLoading != true;
    return Scaffold(
      key: const Key('section-review-loading-page'),
      appBar: AppBar(title: Text(widget.title)),
      body: SafeArea(
        top: false,
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(28, 32, 28, 48),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 460),
              child: failed
                  ? _SectionReviewFailure(
                      message: controller?.errorMessage ?? '本次复盘暂时无法生成。',
                      canRetry: controller?.canRetry == true,
                      onRetry: controller == null
                          ? null
                          : () => unawaited(controller.retry()),
                    )
                  : _SectionReviewLoading(answerCount: widget.answerCount),
            ),
          ),
        ),
      ),
    );
  }
}

class _SectionReviewLoading extends StatefulWidget {
  const _SectionReviewLoading({required this.answerCount});

  final int answerCount;

  @override
  State<_SectionReviewLoading> createState() => _SectionReviewLoadingState();
}

class _SectionReviewLoadingState extends State<_SectionReviewLoading>
    with SingleTickerProviderStateMixin {
  late final AnimationController _animation = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1800),
  )..repeat();

  @override
  void dispose() {
    _animation.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        AnimatedBuilder(
          animation: _animation,
          builder: (context, _) {
            final pulse = (math.sin(_animation.value * math.pi * 2) + 1) / 2;
            return SizedBox(
              key: const Key('section-review-loading-animation'),
              width: 184,
              height: 184,
              child: Stack(
                alignment: Alignment.center,
                children: [
                  Container(
                    width: 148 + pulse * 20,
                    height: 148 + pulse * 20,
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      color: SpeakUpDesign.primaryMuted.withValues(
                        alpha: 0.45 - pulse * 0.2,
                      ),
                    ),
                  ),
                  Transform.rotate(
                    angle: _animation.value * math.pi * 2,
                    child: Container(
                      width: 128,
                      height: 128,
                      decoration: const BoxDecoration(
                        shape: BoxShape.circle,
                        gradient: SweepGradient(
                          colors: [
                            Colors.transparent,
                            SpeakUpDesign.tertiary,
                            SpeakUpDesign.ink,
                            Colors.transparent,
                          ],
                        ),
                      ),
                      padding: const EdgeInsets.all(3),
                      child: const DecoratedBox(
                        decoration: BoxDecoration(
                          shape: BoxShape.circle,
                          color: SpeakUpDesign.canvas,
                        ),
                      ),
                    ),
                  ),
                  Container(
                    width: 92,
                    height: 92,
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      color: SpeakUpDesign.ink,
                      boxShadow: [
                        BoxShadow(
                          color: SpeakUpDesign.ink.withValues(alpha: 0.16),
                          blurRadius: 24,
                          offset: const Offset(0, 10),
                        ),
                      ],
                    ),
                    child: const Icon(
                      Icons.auto_graph_rounded,
                      color: Colors.white,
                      size: 42,
                    ),
                  ),
                ],
              ),
            );
          },
        ),
        const SizedBox(height: 28),
        Text(
          '正在生成你的专项复盘',
          textAlign: TextAlign.center,
          style: SpeakUpDesign.pageTitle.copyWith(fontSize: 25),
        ),
        const SizedBox(height: 10),
        Text(
          '正在整理 ${widget.answerCount} 道回答，分析表达表现并生成下一步建议',
          textAlign: TextAlign.center,
          style: SpeakUpDesign.body.copyWith(height: 1.55),
        ),
        const SizedBox(height: 28),
        const _ReviewLoadingSteps(),
      ],
    );
  }
}

class _ReviewLoadingSteps extends StatelessWidget {
  const _ReviewLoadingSteps();

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 16),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surfaceMuted,
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
      ),
      child: const Row(
        children: [
          _LoadingStep(icon: Icons.notes_rounded, label: '整理回答'),
          _LoadingConnector(),
          _LoadingStep(icon: Icons.radar_rounded, label: '分析表现'),
          _LoadingConnector(),
          _LoadingStep(icon: Icons.lightbulb_outline_rounded, label: '生成建议'),
        ],
      ),
    );
  }
}

class _LoadingStep extends StatelessWidget {
  const _LoadingStep({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Column(
        children: [
          Icon(icon, size: 22, color: SpeakUpDesign.ink),
          const SizedBox(height: 6),
          Text(label, maxLines: 1, style: SpeakUpDesign.meta),
        ],
      ),
    );
  }
}

class _LoadingConnector extends StatelessWidget {
  const _LoadingConnector();

  @override
  Widget build(BuildContext context) {
    return Container(width: 16, height: 1, color: SpeakUpDesign.border);
  }
}

class _SectionReviewFailure extends StatelessWidget {
  const _SectionReviewFailure({
    required this.message,
    required this.canRetry,
    required this.onRetry,
  });

  final String message;
  final bool canRetry;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        const Icon(
          Icons.error_outline_rounded,
          size: 64,
          color: SpeakUpDesign.error,
        ),
        const SizedBox(height: 20),
        Text('复盘暂时没有生成', style: SpeakUpDesign.sectionTitle),
        const SizedBox(height: 10),
        Text(message, textAlign: TextAlign.center, style: SpeakUpDesign.body),
        if (canRetry) ...[
          const SizedBox(height: 24),
          FilledButton(onPressed: onRetry, child: const Text('重新生成报告')),
        ],
      ],
    );
  }
}

class _ResultLine extends StatelessWidget {
  const _ResultLine({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Text(label, style: SpeakUpDesign.body),
        const Spacer(),
        Text(value, style: SpeakUpDesign.cardTitle),
      ],
    );
  }
}

bool _sameProgress(IeltsMockProgress a, IeltsMockProgress b) =>
    a.sessionId == b.sessionId &&
    a.phase == b.phase &&
    a.startedAt == b.startedAt &&
    a.preparationDeadline == b.preparationDeadline &&
    a.speakingStartedAt == b.speakingStartedAt &&
    a.speakingDeadline == b.speakingDeadline &&
    a.part2SpokenSeconds == b.part2SpokenSeconds &&
    a.notes == b.notes &&
    a.deferredTranscriptionStatusUrl == b.deferredTranscriptionStatusUrl;
