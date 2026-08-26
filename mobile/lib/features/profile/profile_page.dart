import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/profile/coaching_profile.dart';
import 'package:speakup/features/coaching/review/review.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';
import 'package:speakup/features/profile/profile_avatar_picker.dart';
import 'package:speakup/features/profile/profile_avatar_view.dart';
import 'package:speakup/features/update/app_update.dart';
import 'package:speakup/features/update/app_update_ui.dart';
import 'package:speakup/identity/client/identity_client.dart';
import 'package:speakup/identity/model/identity_models.dart';

class ProfilePage extends StatelessWidget {
  const ProfilePage({
    required this.showBackButton,
    required this.user,
    required this.profile,
    required this.profileErrorMessage,
    required this.profileSaving,
    required this.onSaveDisplayName,
    required this.avatarBytes,
    required this.avatarSaving,
    required this.onUploadAvatar,
    required this.onUseDefaultAvatar,
    required this.onLogout,
    required this.reviewHistoryController,
    required this.coachingProfileController,
    this.appUpdateService,
    this.updateCheckInProgress = false,
    this.onCheckForUpdate,
    this.avatarPicker,
    super.key,
  });

  final bool showBackButton;
  final User? user;
  final UserProfile? profile;
  final String? profileErrorMessage;
  final bool profileSaving;
  final Future<String?> Function(String)? onSaveDisplayName;
  final Uint8List? avatarBytes;
  final bool avatarSaving;
  final Future<String?> Function(UserAvatarImage)? onUploadAvatar;
  final Future<String?> Function()? onUseDefaultAvatar;
  final VoidCallback? onLogout;
  final ReviewHistoryController? reviewHistoryController;
  final CoachingProfileController? coachingProfileController;
  final AppUpdateService? appUpdateService;
  final bool updateCheckInProgress;
  final Future<void> Function()? onCheckForUpdate;
  final ProfileAvatarPicker? avatarPicker;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('profile-page'),
      backgroundColor: Colors.transparent,
      appBar: showBackButton
          ? AppBar(
              backgroundColor: Colors.transparent,
              leading: IconButton(
                key: const Key('profile-route-back-button'),
                tooltip: '返回',
                onPressed: () => Navigator.of(context).maybePop(),
                icon: const Icon(Icons.arrow_back_rounded),
              ),
            )
          : null,
      body: SafeArea(
        bottom: false,
        child: ListView(
          padding: EdgeInsets.fromLTRB(
            SpeakUpDesign.horizontalInset(context),
            SpeakUpDesign.space32,
            SpeakUpDesign.horizontalInset(context),
            140,
          ),
          children: [
            Center(
              child: Column(
                children: [
                  ProfileAvatarView(
                    avatarKey: const Key('profile-avatar'),
                    size: 132,
                    avatarBytes: avatarBytes,
                    editable: user != null && onUploadAvatar != null,
                    saving: avatarSaving,
                    onTap: () => _editAvatar(context),
                  ),
                  const SizedBox(height: SpeakUpDesign.space20),
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      ConstrainedBox(
                        constraints: const BoxConstraints(maxWidth: 220),
                        child: Text(
                          profile?.displayName ??
                              (user == null ? '本地界面预览' : '尚未设置昵称'),
                          key: const Key('profile-display-name'),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: SpeakUpDesign.pageTitle.copyWith(fontSize: 28),
                        ),
                      ),
                      if (user != null)
                        IconButton(
                          key: const Key('profile-edit-display-name'),
                          tooltip: '编辑昵称',
                          onPressed: profileSaving || onSaveDisplayName == null
                              ? null
                              : () => _editDisplayName(context),
                          icon: Icon(
                            Icons.edit_rounded,
                            size: 18,
                            color: profileSaving
                                ? SpeakUpDesign.tertiary
                                : SpeakUpDesign.secondary,
                          ),
                        ),
                    ],
                  ),
                  const SizedBox(height: SpeakUpDesign.space4),
                  Text(
                    user?.email ?? '尚未连接正式账号',
                    textAlign: TextAlign.center,
                    style: SpeakUpDesign.body.copyWith(
                      color: SpeakUpDesign.tertiary,
                    ),
                  ),
                ],
              ),
            ),
            if (profileErrorMessage != null) ...[
              const SizedBox(height: SpeakUpDesign.space16),
              Text(
                profileErrorMessage!,
                textAlign: TextAlign.center,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: SpeakUpDesign.space32),
            _ProfileSettingsSection(
              coachingProfileController: coachingProfileController,
              historyController: reviewHistoryController,
              onLogout: onLogout,
            ),
            if (appUpdateService != null && onCheckForUpdate != null) ...[
              const SizedBox(height: SpeakUpDesign.space24),
              AppUpdateSection(
                service: appUpdateService!,
                checking: updateCheckInProgress,
                onCheck: onCheckForUpdate!,
              ),
            ],
          ],
        ),
      ),
    );
  }

  Future<void> _editDisplayName(BuildContext context) async {
    final saved = await showDialog<bool>(
      context: context,
      builder: (_) => _DisplayNameDialog(
        initialName: profile?.displayName ?? '',
        onSave: onSaveDisplayName!,
      ),
    );
    if (saved == true && context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('昵称已更新')));
    }
  }

  Future<void> _editAvatar(BuildContext context) async {
    final action = await showModalBottomSheet<_AvatarAction>(
      context: context,
      showDragHandle: true,
      builder: (sheetContext) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(8, 0, 8, 12),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ListTile(
                key: const Key('profile-avatar-gallery'),
                leading: const Icon(Icons.photo_library_outlined),
                title: const Text('从相册选择'),
                onTap: () => Navigator.pop(sheetContext, _AvatarAction.gallery),
              ),
              ListTile(
                key: const Key('profile-avatar-camera'),
                leading: const Icon(Icons.photo_camera_outlined),
                title: const Text('拍照'),
                onTap: () => Navigator.pop(sheetContext, _AvatarAction.camera),
              ),
              ListTile(
                key: const Key('profile-avatar-default'),
                enabled: profile?.avatar != null,
                leading: const Icon(Icons.account_circle_outlined),
                title: const Text('使用默认头像'),
                trailing: profile?.avatar == null
                    ? const Icon(Icons.check_rounded)
                    : null,
                onTap: profile?.avatar == null
                    ? null
                    : () =>
                          Navigator.pop(sheetContext, _AvatarAction.useDefault),
              ),
            ],
          ),
        ),
      ),
    );
    if (action == null || !context.mounted) return;
    if (action == _AvatarAction.useDefault) {
      final error = await onUseDefaultAvatar?.call();
      if (context.mounted) {
        _showAvatarMessage(context, error ?? '已使用默认头像');
      }
      return;
    }
    String? error;
    try {
      final picker = avatarPicker ?? SystemProfileAvatarPicker();
      final image = action == _AvatarAction.gallery
          ? await picker.pickFromGallery()
          : await picker.takePhoto();
      if (image == null || !context.mounted) return;
      error = await onUploadAvatar?.call(image);
    } catch (_) {
      error = '无法读取图片，请检查照片权限后重试。';
    }
    if (context.mounted) {
      _showAvatarMessage(context, error ?? '头像已更新');
    }
  }

  void _showAvatarMessage(BuildContext context, String message) {
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(message)));
  }
}

class _ProfileSettingsSection extends StatefulWidget {
  const _ProfileSettingsSection({
    required this.coachingProfileController,
    required this.historyController,
    required this.onLogout,
  });

  final CoachingProfileController? coachingProfileController;
  final ReviewHistoryController? historyController;
  final VoidCallback? onLogout;

  @override
  State<_ProfileSettingsSection> createState() =>
      _ProfileSettingsSectionState();
}

class _ProfileSettingsSectionState extends State<_ProfileSettingsSection> {
  Future<void>? _coachingProfileLoad;

  @override
  void initState() {
    super.initState();
    _coachingProfileLoad = widget.coachingProfileController?.load();
  }

  @override
  void didUpdateWidget(covariant _ProfileSettingsSection oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(
      oldWidget.coachingProfileController,
      widget.coachingProfileController,
    )) {
      _coachingProfileLoad = widget.coachingProfileController?.load();
    }
  }

  @override
  Widget build(BuildContext context) {
    final hasCoachingProfile = widget.coachingProfileController != null;
    final hasLogout = widget.onLogout != null;
    return Column(
      children: [
        _ProfileSettingsCard(
          children: [
            if (hasCoachingProfile)
              _ProfileSettingsRow(
                key: const Key('profile-coaching-memory-button'),
                icon: Icons.auto_awesome_rounded,
                iconBackground: const LinearGradient(
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                  colors: [Color(0xFF7A5AF8), Color(0xFFA78BFA)],
                ),
                title: '教练记忆',
                subtitle: '管理称呼、背景与沟通偏好',
                onTap: () => unawaited(_openCoachingProfile()),
              ),
            if (hasCoachingProfile) const Divider(indent: 68),
            _ProfileSettingsRow(
              key: const Key('profile-ielts-ability-button'),
              icon: Icons.workspace_premium_rounded,
              iconBackground: const LinearGradient(
                begin: Alignment.topLeft,
                end: Alignment.bottomRight,
                colors: [Color(0xFF1687F8), Color(0xFF55B6FF)],
              ),
              title: 'IELTS 能力',
              subtitle: '查看四维能力与当前估分',
              onTap: _openIeltsAbility,
            ),
          ],
        ),
        if (hasLogout) ...[
          const SizedBox(height: SpeakUpDesign.space16),
          _ProfileSettingsCard(
            children: [
              _ProfileSettingsRow(
                key: const Key('profile-logout-button'),
                icon: Icons.power_settings_new_rounded,
                iconBackground: const LinearGradient(
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                  colors: [Color(0xFFE86B5A), Color(0xFFB83A2B)],
                ),
                title: '退出登录',
                titleColor: SpeakUpDesign.error,
                onTap: widget.onLogout!,
              ),
            ],
          ),
        ],
      ],
    );
  }

  void _openIeltsAbility() {
    unawaited(
      Navigator.of(context).push<void>(
        MaterialPageRoute<void>(
          builder: (_) => CurrentIeltsAbilityPage(
            historyController: widget.historyController,
          ),
        ),
      ),
    );
  }

  Future<void> _openCoachingProfile() async {
    final controller = widget.coachingProfileController;
    if (controller == null) return;
    await _coachingProfileLoad;
    if (!mounted || !identical(controller, widget.coachingProfileController)) {
      return;
    }
    if (controller.profile == null) {
      _coachingProfileLoad = controller.load();
      await _coachingProfileLoad;
    }
    if (!mounted || !identical(controller, widget.coachingProfileController)) {
      return;
    }
    if (controller.profile == null) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(controller.errorMessage ?? '记忆暂时无法读取，请重试。')),
      );
      return;
    }
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => CoachingProfilePage(controller: controller),
      ),
    );
  }
}

class _ProfileSettingsCard extends StatelessWidget {
  const _ProfileSettingsCard({required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: SpeakUpDesign.surface,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
        side: const BorderSide(color: SpeakUpDesign.border),
      ),
      child: Column(children: children),
    );
  }
}

class _ProfileSettingsRow extends StatelessWidget {
  const _ProfileSettingsRow({
    required this.icon,
    required this.iconBackground,
    required this.title,
    required this.onTap,
    this.subtitle,
    this.titleColor,
    super.key,
  });

  final IconData icon;
  final Gradient iconBackground;
  final String title;
  final String? subtitle;
  final Color? titleColor;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      child: ConstrainedBox(
        constraints: const BoxConstraints(minHeight: 76),
        child: Padding(
          padding: const EdgeInsets.symmetric(
            horizontal: SpeakUpDesign.space16,
            vertical: SpeakUpDesign.space12,
          ),
          child: Row(
            children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(
                  gradient: iconBackground,
                  borderRadius: BorderRadius.circular(
                    SpeakUpDesign.radiusControl,
                  ),
                ),
                child: Icon(icon, color: Colors.white, size: 21),
              ),
              const SizedBox(width: SpeakUpDesign.space12),
              Expanded(
                child: Column(
                  mainAxisAlignment: MainAxisAlignment.center,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      style: SpeakUpDesign.cardTitle.copyWith(
                        color: titleColor,
                      ),
                    ),
                    if (subtitle case final value?) ...[
                      const SizedBox(height: SpeakUpDesign.space4),
                      Text(value, style: SpeakUpDesign.meta),
                    ],
                  ],
                ),
              ),
              const SizedBox(width: SpeakUpDesign.space8),
              const Icon(
                Icons.chevron_right_rounded,
                color: SpeakUpDesign.tertiary,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

enum _AvatarAction { gallery, camera, useDefault }

class _DisplayNameDialog extends StatefulWidget {
  const _DisplayNameDialog({required this.initialName, required this.onSave});

  final String initialName;
  final Future<String?> Function(String) onSave;

  @override
  State<_DisplayNameDialog> createState() => _DisplayNameDialogState();
}

class _DisplayNameDialogState extends State<_DisplayNameDialog> {
  late final TextEditingController _controller;
  String? _errorMessage;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _controller = TextEditingController(text: widget.initialName);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('编辑昵称'),
      content: TextField(
        key: const Key('profile-display-name-input'),
        controller: _controller,
        autofocus: true,
        enabled: !_saving,
        maxLength: 40,
        decoration: InputDecoration(labelText: '昵称', errorText: _errorMessage),
      ),
      actions: [
        TextButton(
          onPressed: _saving ? null : () => Navigator.of(context).pop(false),
          child: const Text('取消'),
        ),
        FilledButton(
          key: const Key('profile-save-display-name'),
          onPressed: _saving ? null : _save,
          child: Text(_saving ? '正在保存…' : '保存'),
        ),
      ],
    );
  }

  Future<void> _save() async {
    final value = _controller.text.trim();
    if (value.isEmpty) {
      setState(() => _errorMessage = '请输入昵称');
      return;
    }
    setState(() {
      _saving = true;
      _errorMessage = null;
    });
    final error = await widget.onSave(value);
    if (!mounted) {
      return;
    }
    if (error != null) {
      setState(() {
        _saving = false;
        _errorMessage = error;
      });
      return;
    }
    Navigator.of(context).pop(true);
  }
}
