import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/update/app_update.dart';

class AppUpdateSection extends StatelessWidget {
  const AppUpdateSection({
    required this.service,
    required this.checking,
    required this.onCheck,
    super.key,
  });

  final AppUpdateService service;
  final bool checking;
  final Future<void> Function() onCheck;

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('profile-app-update-section'),
      padding: const EdgeInsets.all(SpeakUpDesign.space20),
      decoration: BoxDecoration(
        border: Border.all(color: SpeakUpDesign.border),
        borderRadius: BorderRadius.circular(22),
      ),
      child: LayoutBuilder(
        builder: (context, constraints) {
          final details = Row(
            children: [
              const Icon(Icons.system_update_alt_rounded, size: 28),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('应用版本', style: SpeakUpDesign.sectionTitle),
                    const SizedBox(height: SpeakUpDesign.space4),
                    FutureBuilder<InstalledAppVersion>(
                      future: service.loadInstalledVersion(),
                      builder: (context, snapshot) {
                        final version = snapshot.data;
                        final label = version == null
                            ? snapshot.hasError
                                  ? '当前版本暂时无法读取'
                                  : '正在读取当前版本…'
                            : '当前 v${version.version}（${version.versionCode}）';
                        return Text(
                          label,
                          key: const Key('profile-app-version'),
                          style: SpeakUpDesign.body,
                        );
                      },
                    ),
                  ],
                ),
              ),
            ],
          );
          final action = TextButton(
            key: const Key('profile-check-update'),
            onPressed: checking ? null : onCheck,
            child: Text(checking ? '检查中…' : '检查更新'),
          );
          final useStackedLayout =
              constraints.maxWidth < 340 ||
              MediaQuery.textScalerOf(context).scale(16) > 22;
          if (useStackedLayout) {
            return Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                details,
                const SizedBox(height: SpeakUpDesign.space8),
                Align(alignment: Alignment.centerRight, child: action),
              ],
            );
          }
          return Row(
            children: [
              Expanded(child: details),
              action,
            ],
          );
        },
      ),
    );
  }
}

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
