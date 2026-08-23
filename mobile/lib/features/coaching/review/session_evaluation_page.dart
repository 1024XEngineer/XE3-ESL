import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/evaluation/session_evaluation_controller.dart';
import 'package:speakup/features/coaching/review/evaluation_report_detail_page.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presentation.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';

class SessionEvaluationPage extends StatefulWidget {
  const SessionEvaluationPage({
    required this.practiceSessionId,
    required this.controller,
    super.key,
  });

  final String practiceSessionId;
  final SessionEvaluationController controller;

  @override
  State<SessionEvaluationPage> createState() => _SessionEvaluationPageState();
}

class _SessionEvaluationPageState extends State<SessionEvaluationPage> {
  @override
  void initState() {
    super.initState();
    unawaited(widget.controller.load(widget.practiceSessionId));
  }

  @override
  void didUpdateWidget(covariant SessionEvaluationPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.practiceSessionId != widget.practiceSessionId ||
        oldWidget.controller != widget.controller) {
      unawaited(widget.controller.load(widget.practiceSessionId));
    }
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: widget.controller,
      builder: (context, _) {
        final report = widget.controller.evaluation?.report;
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
        final error = widget.controller.errorMessage;
        return Scaffold(
          appBar: AppBar(title: const Text('练习复盘')),
          body: SafeArea(
            child: Center(
              child: Padding(
                padding: const EdgeInsets.all(24),
                child: error == null
                    ? const Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          CircularProgressIndicator(),
                          SizedBox(height: 16),
                          Text('正在生成复盘…'),
                        ],
                      )
                    : Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          const Icon(
                            Icons.error_outline_rounded,
                            color: SpeakUpDesign.error,
                            size: 40,
                          ),
                          const SizedBox(height: 12),
                          Text(
                            error,
                            textAlign: TextAlign.center,
                            style: SpeakUpDesign.body,
                          ),
                          if (widget.controller.canRetry) ...[
                            const SizedBox(height: 20),
                            FilledButton(
                              onPressed: () =>
                                  unawaited(widget.controller.retry()),
                              child: const Text('重新生成报告'),
                            ),
                          ],
                        ],
                      ),
              ),
            ),
          ),
        );
      },
    );
  }
}
