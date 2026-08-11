import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/practice_report_status.dart';
import 'package:speakup/features/coaching/review/practice_report_status_controller.dart';

class PracticeReportStatusCard extends StatelessWidget {
  const PracticeReportStatusCard({
    required this.controller,
    required this.onOpenReport,
    super.key,
  });

  final PracticeReportStatusController? controller;
  final Future<void> Function() onOpenReport;

  @override
  Widget build(BuildContext context) {
    final value = controller;
    if (value == null) {
      return const _ReportStatusSurface(
        key: Key('ielts-completion-report-unavailable'),
        icon: Icons.info_outline_rounded,
        title: '复盘状态暂不可用',
        message: '回答已保存，可稍后到复盘页查看。',
      );
    }
    return AnimatedBuilder(
      animation: value,
      builder: (context, _) => _buildStatus(value),
    );
  }

  Widget _buildStatus(PracticeReportStatusController value) {
    final status = value.status;
    if (status == null) {
      if (value.errorMessage case final message?) {
        return _ReportStatusSurface(
          key: const Key('ielts-completion-report-load-failed'),
          icon: Icons.error_outline_rounded,
          title: '暂时无法读取复盘状态',
          message: message,
          action: value.canRetry
              ? TextButton(
                  key: const Key('ielts-completion-report-retry'),
                  onPressed: () => unawaited(value.retry()),
                  child: const Text('重试'),
                )
              : null,
        );
      }
      return const _ReportStatusSurface(
        key: Key('ielts-completion-report-loading'),
        icon: Icons.sync_rounded,
        title: '正在读取复盘状态',
        message: '你可以直接返回训练，不会影响后台处理。',
      );
    }
    return switch (status.evaluationStatus) {
      PracticeReportEvaluationStatus.queued ||
      PracticeReportEvaluationStatus.running => _ReportStatusSurface(
        key: const Key('ielts-completion-report-generating'),
        icon: Icons.schedule_rounded,
        title: status.evaluationStatus == PracticeReportEvaluationStatus.queued
            ? '报告已进入队列'
            : '报告生成中',
        message: value.errorMessage ?? '可先返回训练，完成后可在复盘页查看。',
        action: value.errorMessage == null
            ? null
            : TextButton(
                key: const Key('ielts-completion-report-retry'),
                onPressed: value.canRetry
                    ? () => unawaited(value.retry())
                    : null,
                child: const Text('刷新状态'),
              ),
      ),
      PracticeReportEvaluationStatus.ready => _ReportStatusSurface(
        key: const Key('ielts-completion-report-ready'),
        icon: Icons.description_outlined,
        title: status.scoreability == EvaluationReportScoreability.insufficient
            ? '复盘已生成 · 证据不足'
            : '复盘已生成',
        message: value.errorMessage ?? status.summary!,
        action: FilledButton(
          key: const Key('ielts-completion-report-open'),
          onPressed: value.isLoadingReadyReport
              ? null
              : () => unawaited(onOpenReport()),
          child: Text(
            value.isLoadingReadyReport
                ? '正在打开…'
                : value.errorMessage == null
                ? '查看复盘'
                : '重试打开',
          ),
        ),
      ),
      PracticeReportEvaluationStatus.failed => _ReportStatusSurface(
        key: const Key('ielts-completion-report-failed'),
        icon: Icons.error_outline_rounded,
        title: '报告生成失败',
        message:
            value.errorMessage ??
            _failureMessage(
              status.stableFailure!.reasonCode,
              canRegenerate: value.canRegenerate || value.isRegenerating,
            ),
        action: value.canRegenerate || value.isRegenerating
            ? TextButton(
                key: const Key('ielts-completion-report-retry'),
                onPressed: value.isRegenerating
                    ? null
                    : () => unawaited(value.regenerate()),
                child: Text(value.isRegenerating ? '正在重试…' : '重试生成'),
              )
            : null,
      ),
    };
  }
}

String _failureMessage(String reasonCode, {required bool canRegenerate}) {
  if (!canRegenerate) {
    return '本次报告未能生成，回答已保存，可返回题单再练或稍后查看。';
  }
  return switch (reasonCode.toLowerCase()) {
    'provider_timeout' || 'evaluation_timeout' => '报告生成超时，可以重新生成。',
    _ => '系统暂时无法完成本次报告，可以重新生成。',
  };
}

class _ReportStatusSurface extends StatelessWidget {
  const _ReportStatusSurface({
    required this.icon,
    required this.title,
    required this.message,
    this.action,
    super.key,
  });

  final IconData icon;
  final String title;
  final String message;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: SpeakUpDesign.canvas,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: SpeakUpDesign.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 22, color: SpeakUpDesign.ink),
              const SizedBox(width: 10),
              Expanded(child: Text(title, style: SpeakUpDesign.cardTitle)),
            ],
          ),
          const SizedBox(height: 8),
          Text(message, style: SpeakUpDesign.body),
          if (action case final actionWidget?) ...[
            const SizedBox(height: 12),
            SizedBox(width: double.infinity, child: actionWidget),
          ],
        ],
      ),
    );
  }
}
