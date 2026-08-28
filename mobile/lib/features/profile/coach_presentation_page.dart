import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/profile/coach_presentation_settings.dart';
import 'package:speakup/features/profile/coach_voice_gaze_avatar.dart';
import 'package:speakup/features/profile/coach_voice_selection_page.dart';
import 'package:speakup/platform/audio/ephemeral_wav_audio_player.dart';

const _avatarCarouselInitialPage = 1000;

class CoachPresentationPage extends StatefulWidget {
  const CoachPresentationPage({
    this.accountId = 'preview',
    this.store = const PreviewCoachPresentationSettingsStore(),
    this.voicePreviewPlayerFactory,
    super.key,
  });

  final String accountId;
  final CoachPresentationSettingsStore store;
  final EphemeralWavAudioPlayer Function()? voicePreviewPlayerFactory;

  @override
  State<CoachPresentationPage> createState() => _CoachPresentationPageState();
}

class _CoachPresentationPageState extends State<CoachPresentationPage> {
  final PageController _avatarPageController = PageController(
    initialPage: _avatarCarouselInitialPage,
    viewportFraction: 0.84,
  );
  CoachPresentationCatalog? _catalog;
  String _avatarId = '';
  String _voiceId = '';
  String _savedAvatarId = '';
  String _savedVoiceId = '';
  int _version = 0;
  bool _loading = true;
  bool _saving = false;
  String? _error;
  int _visibleAvatarIndex = 0;
  Future<void>? _saveOperation;

  bool get _changed => _avatarId != _savedAvatarId || _voiceId != _savedVoiceId;

  List<_CoachAvatarOption> get _avatarOptions => _catalog!.avatars
      .map(
        (option) => _CoachAvatarOption(
          id: option.id,
          name: option.displayName,
          description: option.description,
          previewAsset: _previewAsset(option.previewAssetKey),
        ),
      )
      .toList(growable: false);

  List<CoachVoiceOption> get _voiceOptions => _catalog!.voices
      .map(
        (option) => CoachVoiceOption(
          id: option.id,
          name: option.displayName,
          description: option.description,
          gender: option.gender,
        ),
      )
      .toList(growable: false);

  int get _selectedAvatarIndex {
    final index = _avatarOptions.indexWhere((option) => option.id == _avatarId);
    return index < 0 ? 0 : index;
  }

  CoachVoiceOption get _selectedVoice =>
      _voiceOptions.firstWhere((option) => option.id == _voiceId);

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _avatarPageController.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final value = await widget.store.load(accountId: widget.accountId);
      if (!mounted) return;
      setState(() {
        _applyAuthoritativeSettings(value, preserveDesired: false);
        _visibleAvatarIndex = _selectedAvatarIndex;
        _loading = false;
      });
      _jumpToSelectedAvatar();
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = '暂时无法读取设置，请重试。';
        _loading = false;
      });
    }
  }

  Future<void> _save() {
    final active = _saveOperation;
    if (active != null) return active;
    if (!_changed) return Future<void>.value();
    late final Future<void> operation;
    operation = _drainSaves().whenComplete(() {
      if (identical(_saveOperation, operation)) {
        _saveOperation = null;
      }
      if (mounted && _changed) unawaited(_save());
    });
    _saveOperation = operation;
    return operation;
  }

  Future<void> _drainSaves() async {
    setState(() {
      _saving = true;
      _error = null;
    });
    var conflictRetried = false;
    try {
      while (mounted && _changed) {
        final desiredAvatarId = _avatarId;
        final desiredVoiceId = _voiceId;
        try {
          final saved = await widget.store.save(
            accountId: widget.accountId,
            avatarOptionId: desiredAvatarId,
            voiceOptionId: desiredVoiceId,
            expectedVersion: _version,
          );
          if (!mounted) return;
          setState(() {
            _savedAvatarId = saved.avatarOptionId;
            _savedVoiceId = saved.voiceOptionId;
            _version = saved.version;
          });
          conflictRetried = false;
        } on CoachPresentationVersionConflict {
          if (conflictRetried) rethrow;
          final refreshed = await widget.store.refresh(
            accountId: widget.accountId,
          );
          if (!mounted) return;
          setState(() {
            _applyAuthoritativeSettings(refreshed, preserveDesired: true);
          });
          conflictRetried = true;
        }
      }
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _avatarId = _savedAvatarId;
        _voiceId = _savedVoiceId;
        _error = '保存失败，请稍后重试。';
        _visibleAvatarIndex = _selectedAvatarIndex;
      });
      _jumpToSelectedAvatar();
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _selectAvatar(_CoachAvatarOption option) async {
    if (option.id == _avatarId) return;
    setState(() {
      _avatarId = option.id;
      _error = null;
    });
    await _save();
  }

  Future<void> _selectVoice() async {
    await Navigator.of(context).push<void>(
      MaterialPageRoute<void>(
        builder: (_) => CoachVoiceSelectionPage(
          options: _voiceOptions,
          initialVoiceId: _voiceId,
          onSelected: _selectVoiceId,
          loadPreview: (voiceOptionId) => widget.store.previewVoice(
            accountId: widget.accountId,
            voiceOptionId: voiceOptionId,
          ),
          audioPlayerFactory:
              widget.voicePreviewPlayerFactory ?? _createVoicePreviewPlayer,
        ),
      ),
    );
  }

  Future<String> _selectVoiceId(String selectedVoiceId) async {
    if (!mounted || selectedVoiceId == _voiceId) return _voiceId;
    setState(() {
      _voiceId = selectedVoiceId;
      _error = null;
    });
    await _save();
    return _voiceId;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('coach-presentation-page'),
      backgroundColor: Colors.transparent,
      appBar: AppBar(
        backgroundColor: Colors.transparent,
        title: const Text('数字人与音色'),
      ),
      body: SafeArea(
        child: _loading
            ? const Center(child: CircularProgressIndicator())
            : _catalog == null
            ? Center(
                child: Padding(
                  padding: const EdgeInsets.all(SpeakUpDesign.space20),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        _error ?? '暂时无法读取设置，请重试。',
                        textAlign: TextAlign.center,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                      const SizedBox(height: SpeakUpDesign.space12),
                      FilledButton(
                        key: const Key('coach-presentation-retry'),
                        onPressed: () {
                          setState(() {
                            _loading = true;
                            _error = null;
                          });
                          unawaited(_load());
                        },
                        child: const Text('重试'),
                      ),
                    ],
                  ),
                ),
              )
            : ListView(
                padding: const EdgeInsets.fromLTRB(20, 8, 20, 32),
                children: [
                  Text('数字人形象', style: SpeakUpDesign.sectionTitle),
                  const SizedBox(height: 12),
                  _AvatarCarousel(
                    controller: _avatarPageController,
                    options: _avatarOptions,
                    visibleIndex: _visibleAvatarIndex,
                    selectedAvatarId: _avatarId,
                    onPageChanged: (page) {
                      final index = page % _avatarOptions.length;
                      if (index == _visibleAvatarIndex) return;
                      setState(() => _visibleAvatarIndex = index);
                      HapticFeedback.selectionClick();
                    },
                    onSelect: (option) => unawaited(_selectAvatar(option)),
                  ),
                  const SizedBox(height: 28),
                  Text('音色', style: SpeakUpDesign.sectionTitle),
                  const SizedBox(height: 12),
                  _VoiceSelectionEntry(
                    option: _selectedVoice,
                    onTap: _selectVoice,
                  ),
                  if (_saving) ...[
                    const SizedBox(height: 16),
                    const LinearProgressIndicator(
                      key: Key('coach-presentation-saving'),
                    ),
                  ],
                  if (_error != null) ...[
                    const SizedBox(height: 16),
                    Text(
                      _error!,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  ],
                ],
              ),
      ),
    );
  }

  void _applyAuthoritativeSettings(
    CoachPresentationSettings settings, {
    required bool preserveDesired,
  }) {
    final desiredAvatarId = _avatarId;
    final desiredVoiceId = _voiceId;
    _catalog = settings.catalog;
    _savedAvatarId = settings.preference.avatarOptionId;
    _savedVoiceId = settings.preference.voiceOptionId;
    _version = settings.preference.version;
    if (preserveDesired &&
        settings.catalog.contains(desiredAvatarId, desiredVoiceId)) {
      _avatarId = desiredAvatarId;
      _voiceId = desiredVoiceId;
    } else {
      _avatarId = _savedAvatarId;
      _voiceId = _savedVoiceId;
    }
  }

  void _jumpToSelectedAvatar() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !_avatarPageController.hasClients || _catalog == null) {
        return;
      }
      _avatarPageController.jumpToPage(
        _avatarCarouselInitialPage + _selectedAvatarIndex,
      );
    });
  }
}

EphemeralWavAudioPlayer _createVoicePreviewPlayer() =>
    AudioplayersEphemeralWavAudioPlayer();

class _AvatarCarousel extends StatelessWidget {
  const _AvatarCarousel({
    required this.controller,
    required this.options,
    required this.visibleIndex,
    required this.selectedAvatarId,
    required this.onPageChanged,
    required this.onSelect,
  });

  final PageController controller;
  final List<_CoachAvatarOption> options;
  final int visibleIndex;
  final String selectedAvatarId;
  final ValueChanged<int> onPageChanged;
  final ValueChanged<_CoachAvatarOption> onSelect;

  @override
  Widget build(BuildContext context) => Column(
    children: [
      SizedBox(
        height: 370,
        child: PageView.builder(
          key: const Key('coach-avatar-carousel'),
          controller: controller,
          onPageChanged: onPageChanged,
          itemBuilder: (context, page) {
            final index = page % options.length;
            return Padding(
              padding: const EdgeInsets.symmetric(horizontal: 7),
              child: _AvatarCard(
                key: Key('coach-avatar-card-$page'),
                option: options[index],
                selected: options[index].id == selectedAvatarId,
                selectButtonKey: Key('coach-avatar-select-$page'),
                onSelect: () => onSelect(options[index]),
              ),
            );
          },
        ),
      ),
      const SizedBox(height: 14),
      Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: List.generate(
          options.length,
          (index) => AnimatedContainer(
            key: Key('coach-avatar-page-$index'),
            duration: const Duration(milliseconds: 180),
            width: index == visibleIndex ? 24 : 8,
            height: 8,
            margin: const EdgeInsets.symmetric(horizontal: 4),
            decoration: BoxDecoration(
              color: index == visibleIndex
                  ? Colors.black
                  : const Color(0xFFD9DDE1),
              borderRadius: BorderRadius.circular(99),
            ),
          ),
        ),
      ),
      const SizedBox(height: 8),
      Text(
        '左右滑动切换',
        style: SpeakUpDesign.body.copyWith(
          color: SpeakUpDesign.secondary,
          fontSize: 13,
        ),
      ),
    ],
  );
}

class _AvatarCard extends StatelessWidget {
  const _AvatarCard({
    required this.option,
    required this.selected,
    required this.selectButtonKey,
    required this.onSelect,
    super.key,
  });

  final _CoachAvatarOption option;
  final bool selected;
  final Key selectButtonKey;
  final VoidCallback onSelect;

  @override
  Widget build(BuildContext context) => Semantics(
    label: '${option.name}，${option.description}的数字人，点击选择',
    button: true,
    selected: selected,
    child: GestureDetector(
      key: selectButtonKey,
      behavior: HitTestBehavior.opaque,
      onTap: onSelect,
      child: ClipRRect(
        borderRadius: BorderRadius.circular(24),
        child: Stack(
          fit: StackFit.expand,
          children: [
            Image.asset(
              option.previewAsset,
              fit: BoxFit.cover,
              alignment: Alignment.topCenter,
            ),
            const DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.center,
                  end: Alignment.bottomCenter,
                  colors: [Colors.transparent, Color(0xC9000000)],
                  stops: [0.55, 1],
                ),
              ),
            ),
            Positioned(
              left: 22,
              right: 22,
              bottom: 20,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    option.name,
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 28,
                      fontWeight: FontWeight.w700,
                      height: 1.1,
                    ),
                  ),
                  const SizedBox(height: 5),
                  Text(
                    option.description,
                    style: const TextStyle(
                      color: Color(0xFFE5E7EB),
                      fontSize: 16,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                ],
              ),
            ),
            Positioned(
              top: SpeakUpDesign.space20,
              right: SpeakUpDesign.space20,
              child: AnimatedContainer(
                duration: const Duration(milliseconds: 160),
                width: 28,
                height: 28,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: selected
                      ? SpeakUpDesign.ink
                      : Colors.white.withValues(alpha: 0.92),
                  border: Border.all(
                    color: selected
                        ? SpeakUpDesign.ink
                        : SpeakUpDesign.tertiary,
                    width: 1.5,
                  ),
                ),
                child: selected
                    ? const Icon(
                        Icons.check_rounded,
                        color: Colors.white,
                        size: 18,
                      )
                    : null,
              ),
            ),
          ],
        ),
      ),
    ),
  );
}

class _VoiceSelectionEntry extends StatelessWidget {
  const _VoiceSelectionEntry({required this.option, required this.onTap});

  final CoachVoiceOption option;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: SpeakUpDesign.surface,
      borderRadius: BorderRadius.circular(SpeakUpDesign.radiusCard),
      clipBehavior: Clip.antiAlias,
      child: ListTile(
        key: const Key('coach-voice-selection-entry'),
        minTileHeight: 56,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: SpeakUpDesign.space16,
        ),
        leading: CoachVoiceGazeAvatar(
          key: const Key('coach-selected-voice-gaze'),
          voiceId: option.id,
          size: 36,
        ),
        title: Row(
          children: [
            Flexible(
              child: Text(
                option.name,
                style: SpeakUpDesign.cardTitle.copyWith(
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
            const SizedBox(width: SpeakUpDesign.space4),
            CoachVoiceGenderIcon(
              key: const Key('coach-selected-voice-gender'),
              gender: option.gender,
            ),
          ],
        ),
        trailing: const Icon(
          Icons.chevron_right_rounded,
          color: SpeakUpDesign.tertiary,
          size: 24,
        ),
        onTap: onTap,
      ),
    );
  }
}

String _previewAsset(String assetKey) => switch (assetKey) {
  'coach-avatar-lisa' => 'assets/images/avatars/coach-avatar-lisa.webp',
  'coach-avatar-nathan' => 'assets/images/avatars/coach-avatar-nathan.webp',
  _ => throw StateError('Unsupported coach avatar preview asset.'),
};

final class _CoachAvatarOption {
  const _CoachAvatarOption({
    required this.id,
    required this.name,
    required this.description,
    required this.previewAsset,
  });

  final String id;
  final String name;
  final String description;
  final String previewAsset;
}
