import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:speakup/design/speak_up_components.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/update/app_update.dart';
import 'package:url_launcher/url_launcher.dart';

const speakUpAppIconAsset =
    'ios/Runner/Assets.xcassets/AppIcon.appiconset/'
    'Icon-App-1024x1024@1x.png';

final speakUpWebsiteUri = Uri(scheme: 'https', host: 'speak-up.top');

class AboutSpeakUpPage extends StatefulWidget {
  const AboutSpeakUpPage({
    this.appUpdateService,
    this.onCheckForUpdate,
    this.websiteLauncher,
    super.key,
  });

  final AppUpdateService? appUpdateService;
  final Future<AppUpdateCheckResult?> Function()? onCheckForUpdate;
  final Future<bool> Function(Uri uri)? websiteLauncher;

  @override
  State<AboutSpeakUpPage> createState() => _AboutSpeakUpPageState();
}

class _AboutSpeakUpPageState extends State<AboutSpeakUpPage> {
  late Future<InstalledAppVersion> _installedVersion;
  AppUpdateCheckStatus? _updateStatus;
  bool _checking = false;

  @override
  void initState() {
    super.initState();
    _installedVersion = _loadInstalledVersion();
  }

  @override
  void didUpdateWidget(covariant AboutSpeakUpPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.appUpdateService, widget.appUpdateService)) {
      _installedVersion = _loadInstalledVersion();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('about-speak-up-page'),
      backgroundColor: SpeakUpDesign.ambientBase,
      appBar: AppBar(
        backgroundColor: SpeakUpDesign.surface,
        surfaceTintColor: Colors.transparent,
        centerTitle: true,
        systemOverlayStyle: SystemUiOverlayStyle.dark,
        title: const Text('关于 SpeakUp'),
      ),
      body: CustomScrollView(
        slivers: [
          SliverPadding(
            padding: const EdgeInsets.fromLTRB(
              SpeakUpDesign.space16,
              SpeakUpDesign.space32,
              SpeakUpDesign.space16,
              0,
            ),
            sliver: SliverList.list(
              children: [
                _BrandHeader(version: _installedVersion),
                const SizedBox(height: 36),
                _AboutActionsCard(
                  checking: _checking,
                  updateStatus: _updateStatus,
                  updateAvailable: widget.onCheckForUpdate != null,
                  onCheckForUpdate: _checkForUpdate,
                  onUpdateUnavailable: _showPlatformUpdateInfo,
                  onOpenWebsite: _openWebsite,
                  onOpenLicenses: _openLicenses,
                ),
              ],
            ),
          ),
          SliverFillRemaining(
            hasScrollBody: false,
            child: Padding(
              padding: EdgeInsets.fromLTRB(
                SpeakUpDesign.space16,
                SpeakUpDesign.space32,
                SpeakUpDesign.space16,
                MediaQuery.viewPaddingOf(context).bottom +
                    SpeakUpDesign.space24,
              ),
              child: Align(
                alignment: Alignment.bottomCenter,
                child: Text(
                  '© 2026 SpeakUp\nspeak-up.top',
                  textAlign: TextAlign.center,
                  style: SpeakUpDesign.body.copyWith(
                    color: SpeakUpDesign.secondary,
                    fontSize: 13,
                    height: 1.5,
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Future<InstalledAppVersion> _loadInstalledVersion() {
    return widget.appUpdateService?.loadInstalledVersion() ??
        loadInstalledAppVersion();
  }

  Future<void> _checkForUpdate() async {
    final check = widget.onCheckForUpdate;
    if (check == null || _checking) return;
    setState(() => _checking = true);
    try {
      final result = await check();
      if (mounted && result != null) {
        setState(() => _updateStatus = result.status);
      }
    } finally {
      if (mounted) {
        setState(() => _checking = false);
      }
    }
  }

  void _showPlatformUpdateInfo() {
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('当前平台请通过应用商店更新')));
  }

  Future<void> _openWebsite() async {
    final launcher = widget.websiteLauncher ?? _launchExternalUri;
    var opened = false;
    try {
      opened = await launcher(speakUpWebsiteUri);
    } on Object {
      opened = false;
    }
    if (!opened && mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('暂时无法打开产品官网')));
    }
  }

  Future<void> _openLicenses() async {
    InstalledAppVersion? installed;
    try {
      installed = await _installedVersion;
    } on Object {
      installed = null;
    }
    if (!mounted) return;
    showLicensePage(
      context: context,
      applicationName: 'SpeakUp',
      applicationVersion: installed == null
          ? null
          : 'v${installed.version} (${installed.versionCode})',
      applicationIcon: ClipRRect(
        borderRadius: BorderRadius.circular(16),
        child: Image.asset(
          speakUpAppIconAsset,
          width: 64,
          height: 64,
          fit: BoxFit.cover,
        ),
      ),
    );
  }
}

class _BrandHeader extends StatelessWidget {
  const _BrandHeader({required this.version});

  final Future<InstalledAppVersion> version;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        ClipRRect(
          key: const Key('about-speak-up-app-icon'),
          borderRadius: BorderRadius.circular(24),
          child: Image.asset(
            speakUpAppIconAsset,
            width: 104,
            height: 104,
            fit: BoxFit.cover,
          ),
        ),
        const SizedBox(height: SpeakUpDesign.space16),
        const SpeakUpWordmark(key: Key('about-speak-up-wordmark'), height: 54),
        const SizedBox(height: SpeakUpDesign.space12),
        FutureBuilder<InstalledAppVersion>(
          future: version,
          builder: (context, snapshot) {
            final installed = snapshot.data;
            final label = installed == null
                ? snapshot.hasError
                      ? '版本信息暂不可用'
                      : '正在读取版本信息…'
                : '版本 v${installed.version} (${installed.versionCode})';
            return Text(
              label,
              key: const Key('profile-app-version'),
              textAlign: TextAlign.center,
              style: SpeakUpDesign.body.copyWith(
                color: SpeakUpDesign.secondary,
                fontSize: 14,
                height: 1.3,
              ),
            );
          },
        ),
      ],
    );
  }
}

class _AboutActionsCard extends StatelessWidget {
  const _AboutActionsCard({
    required this.checking,
    required this.updateStatus,
    required this.updateAvailable,
    required this.onCheckForUpdate,
    required this.onUpdateUnavailable,
    required this.onOpenWebsite,
    required this.onOpenLicenses,
  });

  final bool checking;
  final AppUpdateCheckStatus? updateStatus;
  final bool updateAvailable;
  final VoidCallback onCheckForUpdate;
  final VoidCallback onUpdateUnavailable;
  final VoidCallback onOpenWebsite;
  final VoidCallback onOpenLicenses;

  @override
  Widget build(BuildContext context) {
    return Material(
      key: const Key('about-speak-up-actions'),
      color: SpeakUpDesign.surface,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(14),
        side: const BorderSide(color: SpeakUpDesign.border),
      ),
      child: Column(
        children: [
          _AboutActionRow(
            key: const Key('profile-check-update'),
            title: '检查更新',
            onTap: updateAvailable ? onCheckForUpdate : onUpdateUnavailable,
            trailing: _UpdateStatus(
              checking: checking,
              status: updateStatus,
              updateAvailable: updateAvailable,
            ),
          ),
          const Divider(height: 1),
          _AboutActionRow(
            key: const Key('about-speak-up-website'),
            title: '产品官网',
            onTap: onOpenWebsite,
          ),
          const Divider(height: 1),
          _AboutActionRow(
            key: const Key('about-speak-up-licenses'),
            title: '开源许可',
            onTap: onOpenLicenses,
          ),
        ],
      ),
    );
  }
}

class _AboutActionRow extends StatelessWidget {
  const _AboutActionRow({
    required this.title,
    required this.onTap,
    this.trailing,
    super.key,
  });

  final String title;
  final VoidCallback? onTap;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      minTileHeight: 56,
      minVerticalPadding: SpeakUpDesign.space8,
      contentPadding: const EdgeInsets.symmetric(
        horizontal: SpeakUpDesign.space16,
      ),
      onTap: onTap,
      title: Text(
        title,
        style: SpeakUpDesign.cardTitle.copyWith(
          fontSize: 16,
          fontWeight: FontWeight.w500,
        ),
      ),
      trailing:
          trailing ??
          const Icon(
            Icons.chevron_right_rounded,
            color: SpeakUpDesign.tertiary,
            size: 20,
          ),
    );
  }
}

class _UpdateStatus extends StatelessWidget {
  const _UpdateStatus({
    required this.checking,
    required this.status,
    required this.updateAvailable,
  });

  final bool checking;
  final AppUpdateCheckStatus? status;
  final bool updateAvailable;

  @override
  Widget build(BuildContext context) {
    if (checking) {
      return const SizedBox.square(
        dimension: 18,
        child: CircularProgressIndicator(strokeWidth: 2),
      );
    }
    final label = switch (status) {
      AppUpdateCheckStatus.upToDate => '已是最新版本',
      AppUpdateCheckStatus.available => '发现新版本',
      AppUpdateCheckStatus.failed => '检查失败',
      AppUpdateCheckStatus.skipped => '稍后再试',
      null => updateAvailable ? '检查' : '当前版本',
    };
    return Text(
      label,
      style: SpeakUpDesign.body.copyWith(
        color: SpeakUpDesign.secondary,
        fontSize: 14,
        height: 1.3,
      ),
    );
  }
}

Future<bool> _launchExternalUri(Uri uri) {
  return launchUrl(uri, mode: LaunchMode.externalApplication);
}
