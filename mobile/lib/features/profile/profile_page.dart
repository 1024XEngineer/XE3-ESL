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
      appBar: showBackButton
          ? AppBar(
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
            SpeakUpDesign.space24,
            SpeakUpDesign.horizontalInset(context),
            140,
          ),
          children: [
            Stack(
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
                              style: SpeakUpDesign.pageTitle.copyWith(
                                fontSize: 28,
                              ),
                            ),
                          ),
                          if (user != null)
                            IconButton(
                              key: const Key('profile-edit-display-name'),
                              tooltip: '编辑昵称',
                              onPressed:
                                  profileSaving || onSaveDisplayName == null
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
                if (user != null &&
                    (onLogout != null || coachingProfileController != null))
                  Align(
                    alignment: Alignment.topRight,
                    child: _ProfileMoreMenu(
                      coachingProfileController: coachingProfileController,
                      onLogout: onLogout,
                    ),
                  ),
              ],
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
            CurrentIeltsAbilityProfile(
              historyController: reviewHistoryController,
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

class _ProfileMoreMenu extends StatefulWidget {
  const _ProfileMoreMenu({
    required this.coachingProfileController,
    required this.onLogout,
  });

  final CoachingProfileController? coachingProfileController;
  final VoidCallback? onLogout;

  @override
  State<_ProfileMoreMenu> createState() => _ProfileMoreMenuState();
}

class _ProfileMoreMenuState extends State<_ProfileMoreMenu> {
  Future<void>? _coachingProfileLoad;

  @override
  void initState() {
    super.initState();
    _coachingProfileLoad = widget.coachingProfileController?.load();
  }

  @override
  void didUpdateWidget(covariant _ProfileMoreMenu oldWidget) {
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
    return MenuAnchor(
      key: const Key('profile-more-menu-anchor'),
      animated: true,
      reservedPadding: const EdgeInsets.all(SpeakUpDesign.space16),
      alignmentOffset: const Offset(-184, SpeakUpDesign.space4),
      style: MenuStyle(
        alignment: AlignmentDirectional.bottomEnd,
        backgroundColor: const WidgetStatePropertyAll(SpeakUpDesign.surface),
        surfaceTintColor: const WidgetStatePropertyAll(Colors.transparent),
        shadowColor: const WidgetStatePropertyAll(Color(0x26000000)),
        elevation: const WidgetStatePropertyAll(6),
        padding: const WidgetStatePropertyAll(
          EdgeInsets.fromLTRB(
            SpeakUpDesign.space8,
            SpeakUpDesign.space16,
            SpeakUpDesign.space8,
            SpeakUpDesign.space8,
          ),
        ),
        minimumSize: const WidgetStatePropertyAll(Size(184, 0)),
        maximumSize: const WidgetStatePropertyAll(Size(184, double.infinity)),
        side: const WidgetStatePropertyAll(
          BorderSide(color: SpeakUpDesign.border),
        ),
        shape: const WidgetStatePropertyAll(_ProfileMenuCalloutBorder()),
      ),
      menuChildren: [
        if (hasCoachingProfile)
          MenuItemButton(
            key: const Key('profile-coaching-memory-button'),
            leadingIcon: const Icon(Icons.psychology_alt_outlined, size: 20),
            style: _menuItemStyle(),
            onPressed: () => unawaited(_openCoachingProfile()),
            child: const Text('教练记忆'),
          ),
        if (hasCoachingProfile && hasLogout)
          const Divider(
            height: 1,
            indent: SpeakUpDesign.space12,
            endIndent: SpeakUpDesign.space12,
          ),
        if (hasLogout)
          MenuItemButton(
            key: const Key('profile-logout-button'),
            leadingIcon: const Icon(Icons.logout_rounded, size: 20),
            style: _menuItemStyle(destructive: true),
            onPressed: widget.onLogout,
            child: const Text('退出登录'),
          ),
      ],
      builder: (context, controller, _) => IconButton(
        key: const Key('profile-account-menu'),
        tooltip: '更多',
        constraints: const BoxConstraints.tightFor(
          width: SpeakUpDesign.minTapTarget,
          height: SpeakUpDesign.minTapTarget,
        ),
        style: ButtonStyle(
          tapTargetSize: MaterialTapTargetSize.shrinkWrap,
          shape: WidgetStatePropertyAll(
            RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
            ),
          ),
          overlayColor: const WidgetStatePropertyAll(
            SpeakUpDesign.primaryMuted,
          ),
        ),
        onPressed: hasCoachingProfile || hasLogout
            ? () => controller.isOpen ? controller.close() : controller.open()
            : null,
        icon: const Icon(Icons.more_horiz_rounded),
      ),
    );
  }

  ButtonStyle _menuItemStyle({bool destructive = false}) {
    final foreground = destructive ? SpeakUpDesign.error : SpeakUpDesign.ink;
    return ButtonStyle(
      minimumSize: const WidgetStatePropertyAll(Size(168, 48)),
      padding: const WidgetStatePropertyAll(
        EdgeInsets.symmetric(horizontal: SpeakUpDesign.space12),
      ),
      alignment: Alignment.centerLeft,
      foregroundColor: WidgetStatePropertyAll(foreground),
      iconColor: WidgetStatePropertyAll(foreground),
      textStyle: WidgetStatePropertyAll(
        SpeakUpDesign.body.copyWith(fontWeight: FontWeight.w600),
      ),
      shape: WidgetStatePropertyAll(
        RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
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

class _ProfileMenuCalloutBorder extends OutlinedBorder {
  const _ProfileMenuCalloutBorder({
    super.side,
    this.radius = SpeakUpDesign.radiusCard,
    this.arrowHeight = SpeakUpDesign.space8,
    this.arrowWidth = SpeakUpDesign.space16,
    this.arrowEndInset = SpeakUpDesign.minTapTarget / 2,
  });

  final double radius;
  final double arrowHeight;
  final double arrowWidth;
  final double arrowEndInset;

  @override
  EdgeInsetsGeometry get dimensions {
    final inset = side.strokeInset > 0 ? side.strokeInset : 0.0;
    return EdgeInsets.fromLTRB(inset, arrowHeight + inset, inset, inset);
  }

  @override
  _ProfileMenuCalloutBorder copyWith({
    BorderSide? side,
    double? radius,
    double? arrowHeight,
    double? arrowWidth,
    double? arrowEndInset,
  }) => _ProfileMenuCalloutBorder(
    side: side ?? this.side,
    radius: radius ?? this.radius,
    arrowHeight: arrowHeight ?? this.arrowHeight,
    arrowWidth: arrowWidth ?? this.arrowWidth,
    arrowEndInset: arrowEndInset ?? this.arrowEndInset,
  );

  @override
  ShapeBorder scale(double t) => _ProfileMenuCalloutBorder(
    side: side.scale(t),
    radius: radius * t,
    arrowHeight: arrowHeight * t,
    arrowWidth: arrowWidth * t,
    arrowEndInset: arrowEndInset * t,
  );

  @override
  Path getInnerPath(Rect rect, {TextDirection? textDirection}) {
    return getOuterPath(rect.deflate(side.strokeInset));
  }

  @override
  Path getOuterPath(Rect rect, {TextDirection? textDirection}) {
    final bodyTop = rect.top + arrowHeight;
    final body = Path()
      ..addRRect(
        RRect.fromRectAndRadius(
          Rect.fromLTRB(rect.left, bodyTop, rect.right, rect.bottom),
          Radius.circular(radius),
        ),
      );
    final arrowCenter = rect.right - arrowEndInset;
    final arrow = Path()
      ..moveTo(arrowCenter - arrowWidth / 2, bodyTop)
      ..lineTo(arrowCenter, rect.top)
      ..lineTo(arrowCenter + arrowWidth / 2, bodyTop)
      ..close();
    return Path.combine(PathOperation.union, body, arrow);
  }

  @override
  void paint(Canvas canvas, Rect rect, {TextDirection? textDirection}) {
    if (side.style == BorderStyle.none) return;
    canvas.drawPath(
      getOuterPath(rect.deflate(side.strokeInset)),
      side.toPaint(),
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
