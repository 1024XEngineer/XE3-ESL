import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/profile/coach_voice_selection_page.dart';

const _femaleAvatarId = '94a60c13-e835-4bde-aa93-00a1cf178dcd';
const _maleAvatarId = '1843ff9f-db3a-45de-be28-9c2b9d6412a3';
const _femaleVoiceId = 'loongeva_v3.6';
const _maleVoiceId = 'loongjohn';
const _avatarCarouselInitialPage = 1000;

const _avatarOptions = <_CoachAvatarOption>[
  _CoachAvatarOption(
    id: _femaleAvatarId,
    name: '莉萨',
    description: '亲切、开朗',
    previewAsset: 'assets/images/avatars/coach-avatar-lisa.webp',
  ),
  _CoachAvatarOption(
    id: _maleAvatarId,
    name: '内森',
    description: '温暖、沉稳',
    previewAsset: 'assets/images/avatars/coach-avatar-nathan.webp',
  ),
];

const _voiceOptions = <CoachVoiceOption>[
  CoachVoiceOption(
    id: _femaleVoiceId,
    name: '艾娃',
    description: '清晰自然 · 美式英语 · 女声',
  ),
  CoachVoiceOption(
    id: _maleVoiceId,
    name: '约翰',
    description: '温暖沉稳 · 美式英语 · 男声',
  ),
];

abstract interface class CoachPresentationSettingsStore {
  Future<({String avatarId, String voiceId})> load();

  Future<void> save({required String avatarId, required String voiceId});
}

final class SecureCoachPresentationSettingsStore
    implements CoachPresentationSettingsStore {
  const SecureCoachPresentationSettingsStore()
    : _storage = const FlutterSecureStorage();

  static const _avatarKey = 'coach_avatar_id';
  static const _voiceKey = 'coach_voice_id';
  final FlutterSecureStorage _storage;

  @override
  Future<({String avatarId, String voiceId})> load() async {
    final values = await Future.wait<String?>([
      _storage.read(key: _avatarKey),
      _storage.read(key: _voiceKey),
    ]);
    return (
      avatarId: _validAvatar(values[0]) ? values[0]! : _femaleAvatarId,
      voiceId: _validVoice(values[1]) ? values[1]! : _femaleVoiceId,
    );
  }

  @override
  Future<void> save({required String avatarId, required String voiceId}) async {
    if (!_validAvatar(avatarId) || !_validVoice(voiceId)) {
      throw ArgumentError('Unsupported coach presentation.');
    }
    await Future.wait<void>([
      _storage.write(key: _avatarKey, value: avatarId),
      _storage.write(key: _voiceKey, value: voiceId),
    ]);
  }
}

class CoachPresentationPage extends StatefulWidget {
  const CoachPresentationPage({
    this.store = const SecureCoachPresentationSettingsStore(),
    super.key,
  });

  final CoachPresentationSettingsStore store;

  @override
  State<CoachPresentationPage> createState() => _CoachPresentationPageState();
}

class _CoachPresentationPageState extends State<CoachPresentationPage> {
  final PageController _avatarPageController = PageController(
    initialPage: _avatarCarouselInitialPage,
    viewportFraction: 0.84,
  );
  String _avatarId = _femaleAvatarId;
  String _voiceId = _femaleVoiceId;
  String _savedAvatarId = _femaleAvatarId;
  String _savedVoiceId = _femaleVoiceId;
  bool _loading = true;
  bool _saving = false;
  String? _error;

  bool get _changed => _avatarId != _savedAvatarId || _voiceId != _savedVoiceId;

  int get _selectedAvatarIndex =>
      _avatarOptions.indexWhere((option) => option.id == _avatarId);

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
      final value = await widget.store.load();
      if (!mounted) return;
      setState(() {
        _avatarId = value.avatarId;
        _voiceId = value.voiceId;
        _savedAvatarId = value.avatarId;
        _savedVoiceId = value.voiceId;
        _loading = false;
      });
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !_avatarPageController.hasClients) return;
        _avatarPageController.jumpToPage(
          _avatarCarouselInitialPage + _selectedAvatarIndex,
        );
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = '暂时无法读取设置，请重试。';
        _loading = false;
      });
    }
  }

  Future<void> _save() async {
    if (_saving || !_changed) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await widget.store.save(avatarId: _avatarId, voiceId: _voiceId);
      if (!mounted) return;
      setState(() {
        _savedAvatarId = _avatarId;
        _savedVoiceId = _voiceId;
        _saving = false;
      });
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('设置已保存')));
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = '保存失败，请稍后重试。';
      });
    }
  }

  Future<void> _selectVoice() async {
    final selectedVoiceId = await Navigator.of(context).push<String>(
      MaterialPageRoute<String>(
        builder: (_) => CoachVoiceSelectionPage(
          options: _voiceOptions,
          initialVoiceId: _voiceId,
        ),
      ),
    );
    if (!mounted || selectedVoiceId == null || selectedVoiceId == _voiceId) {
      return;
    }
    setState(() => _voiceId = selectedVoiceId);
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
            : ListView(
                padding: const EdgeInsets.fromLTRB(20, 8, 20, 32),
                children: [
                  Text(
                    '选择你喜欢的陪练形象和声音',
                    style: SpeakUpDesign.body.copyWith(
                      color: SpeakUpDesign.secondary,
                    ),
                  ),
                  const SizedBox(height: 28),
                  Text('数字人形象', style: SpeakUpDesign.sectionTitle),
                  const SizedBox(height: 12),
                  _AvatarCarousel(
                    controller: _avatarPageController,
                    selectedIndex: _selectedAvatarIndex,
                    onPageChanged: (page) {
                      final option =
                          _avatarOptions[page % _avatarOptions.length];
                      if (option.id == _avatarId) return;
                      setState(() => _avatarId = option.id);
                      HapticFeedback.selectionClick();
                    },
                  ),
                  const SizedBox(height: 28),
                  Text('音色', style: SpeakUpDesign.sectionTitle),
                  const SizedBox(height: 12),
                  _VoiceSelectionEntry(
                    option: _selectedVoice,
                    onTap: _selectVoice,
                  ),
                  if (_error != null) ...[
                    const SizedBox(height: 16),
                    Text(
                      _error!,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  ],
                  const SizedBox(height: 28),
                  SizedBox(
                    height: 52,
                    child: FilledButton(
                      key: const Key('coach-presentation-save'),
                      onPressed: _changed && !_saving ? _save : null,
                      child: _saving
                          ? const SizedBox.square(
                              dimension: 20,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : const Text('保存设置'),
                    ),
                  ),
                ],
              ),
      ),
    );
  }
}

class _AvatarCarousel extends StatelessWidget {
  const _AvatarCarousel({
    required this.controller,
    required this.selectedIndex,
    required this.onPageChanged,
  });

  final PageController controller;
  final int selectedIndex;
  final ValueChanged<int> onPageChanged;

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
            final index = page % _avatarOptions.length;
            return Padding(
              padding: const EdgeInsets.symmetric(horizontal: 7),
              child: _AvatarCard(
                key: Key('coach-avatar-card-$page'),
                option: _avatarOptions[index],
              ),
            );
          },
        ),
      ),
      const SizedBox(height: 14),
      Row(
        mainAxisAlignment: MainAxisAlignment.center,
        children: List.generate(
          _avatarOptions.length,
          (index) => AnimatedContainer(
            key: Key('coach-avatar-page-$index'),
            duration: const Duration(milliseconds: 180),
            width: index == selectedIndex ? 24 : 8,
            height: 8,
            margin: const EdgeInsets.symmetric(horizontal: 4),
            decoration: BoxDecoration(
              color: index == selectedIndex
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
  const _AvatarCard({required this.option, super.key});

  final _CoachAvatarOption option;

  @override
  Widget build(BuildContext context) => Semantics(
    label: '${option.name}，${option.description}的数字人',
    image: true,
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
        ],
      ),
    ),
  );
}

class _VoiceSelectionEntry extends StatelessWidget {
  const _VoiceSelectionEntry({required this.option, required this.onTap});

  final CoachVoiceOption option;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: SpeakUpDesign.surface,
      borderRadius: BorderRadius.circular(20),
      clipBehavior: Clip.antiAlias,
      child: ListTile(
        key: const Key('coach-voice-selection-entry'),
        minTileHeight: 76,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: SpeakUpDesign.space16,
          vertical: SpeakUpDesign.space8,
        ),
        leading: const CircleAvatar(
          backgroundColor: Color(0xFFF0F3F5),
          foregroundColor: SpeakUpDesign.ink,
          child: Icon(Icons.graphic_eq_rounded),
        ),
        title: Text(option.name, style: SpeakUpDesign.cardTitle),
        subtitle: Text(
          option.description,
          style: SpeakUpDesign.body.copyWith(fontSize: 13),
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

bool _validAvatar(String? value) =>
    value == _femaleAvatarId || value == _maleAvatarId;

bool _validVoice(String? value) =>
    value == _femaleVoiceId || value == _maleVoiceId;

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
