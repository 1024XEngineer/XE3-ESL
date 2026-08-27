import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/update/app_update.dart';

Future<bool?> showAppUpdateDialog(
  BuildContext context, {
  required InstalledAppVersion installedVersion,
  required AppRelease release,
}) {
  return showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      key: const Key('app-update-dialog'),
      scrollable: true,
      title: Text('发现新版本 v${release.version}'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('当前版本：v${installedVersion.version}'),
          const SizedBox(height: SpeakUpDesign.space8),
          Text('安装包：${formatAppUpdateBytes(release.sizeBytes)}'),
          const SizedBox(height: SpeakUpDesign.space8),
          Text('发布时间：${formatAppUpdateDate(release.publishedAt)}'),
          const SizedBox(height: SpeakUpDesign.space16),
          Text('点击后将在系统浏览器中下载，安装时请按 Android 提示确认。', style: SpeakUpDesign.meta),
        ],
      ),
      actions: [
        TextButton(
          key: const Key('app-update-later'),
          onPressed: () => Navigator.of(dialogContext).pop(false),
          child: const Text('稍后'),
        ),
        FilledButton(
          key: const Key('app-update-now'),
          onPressed: () => Navigator.of(dialogContext).pop(true),
          child: const Text('立即更新'),
        ),
      ],
    ),
  );
}

String formatAppUpdateBytes(int bytes) {
  return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
}

String formatAppUpdateDate(DateTime value) {
  final local = value.toLocal();
  return '${local.year}-${_twoDigits(local.month)}-${_twoDigits(local.day)}';
}

String _twoDigits(int value) => value.toString().padLeft(2, '0');
