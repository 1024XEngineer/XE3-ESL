import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/coaching/profile/coaching_profile.dart';
import 'package:speakup/features/coaching/review/review.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';
import 'package:speakup/identity/model/identity_models.dart';

class ProfilePage extends StatelessWidget {
  const ProfilePage({
    required this.showBackButton,
    required this.user,
    required this.profile,
    required this.profileErrorMessage,
    required this.profileSaving,
    required this.onSaveDisplayName,
    required this.onLogout,
    required this.reviewHistoryController,
    required this.coachingProfileController,
    super.key,
  });

  final bool showBackButton;
  final User? user;
  final UserProfile? profile;
  final String? profileErrorMessage;
  final bool profileSaving;
  final Future<String?> Function(String)? onSaveDisplayName;
  final VoidCallback? onLogout;
  final ReviewHistoryController? reviewHistoryController;
  final CoachingProfileController? coachingProfileController;

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
                      Container(
                        key: const Key('profile-avatar'),
                        width: 132,
                        height: 132,
                        padding: const EdgeInsets.all(3),
                        decoration: const BoxDecoration(
                          color: SpeakUpDesign.surface,
                          shape: BoxShape.circle,
                          boxShadow: [
                            BoxShadow(
                              color: Color(0x14000000),
                              blurRadius: 20,
                              offset: Offset(0, 8),
                            ),
                          ],
                        ),
                        child: const CircleAvatar(
                          backgroundColor: SpeakUpDesign.surfaceMuted,
                          backgroundImage: AssetImage(
                            'assets/images/scenes/profile-avatar-alex.png',
                          ),
                        ),
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
                if (user != null)
                  Align(
                    alignment: Alignment.topRight,
                    child: PopupMenuButton<String>(
                      key: const Key('profile-account-menu'),
                      tooltip: '账号菜单',
                      enabled: onLogout != null,
                      icon: const Icon(Icons.more_horiz_rounded),
                      onSelected: (_) => onLogout?.call(),
                      itemBuilder: (_) => const [
                        PopupMenuItem<String>(
                          key: Key('profile-logout-button'),
                          value: 'logout',
                          child: Text('退出登录'),
                        ),
                      ],
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
            if (coachingProfileController != null) ...[
              CoachingProfileCard(controller: coachingProfileController!),
              const SizedBox(height: SpeakUpDesign.space24),
            ],
            CurrentIeltsAbilityProfile(
              historyController: reviewHistoryController,
            ),
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
}

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
