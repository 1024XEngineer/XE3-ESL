import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';

class ProfileAvatarView extends StatelessWidget {
  const ProfileAvatarView({
    required this.size,
    this.avatarBytes,
    this.editable = false,
    this.saving = false,
    this.onTap,
    this.avatarKey,
    super.key,
  });

  final double size;
  final Uint8List? avatarBytes;
  final bool editable;
  final bool saving;
  final VoidCallback? onTap;
  final Key? avatarKey;

  @override
  Widget build(BuildContext context) {
    final avatar = Container(
      key: avatarKey,
      width: size,
      height: size,
      padding: EdgeInsets.all(size >= 100 ? 3 : 0),
      decoration: BoxDecoration(
        color: SpeakUpDesign.surface,
        shape: BoxShape.circle,
        boxShadow: size >= 100
            ? const [
                BoxShadow(
                  color: Color(0x14000000),
                  blurRadius: 20,
                  offset: Offset(0, 8),
                ),
              ]
            : null,
      ),
      child: ClipOval(
        child: avatarBytes == null
            ? Image.asset(
                'assets/images/scenes/profile-avatar-alex.png',
                fit: BoxFit.cover,
              )
            : Image.memory(
                avatarBytes!,
                fit: BoxFit.cover,
                errorBuilder: (_, _, _) => Image.asset(
                  'assets/images/scenes/profile-avatar-alex.png',
                  fit: BoxFit.cover,
                ),
              ),
      ),
    );
    if (!editable) {
      return avatar;
    }
    final badgeSize = (size * 0.32).clamp(28.0, 36.0).toDouble();
    return Semantics(
      button: true,
      label: '修改头像',
      child: InkWell(
        key: const Key('profile-avatar-edit-button'),
        customBorder: const CircleBorder(),
        onTap: saving ? null : onTap,
        child: Stack(
          clipBehavior: Clip.none,
          children: [
            avatar,
            Positioned(
              right: 2,
              bottom: 2,
              child: Container(
                width: badgeSize,
                height: badgeSize,
                decoration: BoxDecoration(
                  color: SpeakUpDesign.primary,
                  shape: BoxShape.circle,
                  border: Border.all(color: SpeakUpDesign.surface, width: 2),
                ),
                child: saving
                    ? const Padding(
                        padding: EdgeInsets.all(8),
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: SpeakUpDesign.canvas,
                        ),
                      )
                    : const Icon(
                        Icons.photo_camera_rounded,
                        color: SpeakUpDesign.canvas,
                        size: 17,
                      ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
