import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/profile/coach_voice_gaze_avatar.dart';
import 'package:speakup/platform/audio/ephemeral_wav_audio_player.dart';

final class CoachVoiceOption {
  const CoachVoiceOption({
    required this.id,
    required this.name,
    required this.description,
    required this.gender,
  });

  final String id;
  final String name;
  final String description;
  final String gender;
}

class CoachVoiceSelectionPage extends StatefulWidget {
  const CoachVoiceSelectionPage({
    required this.options,
    required this.initialVoiceId,
    required this.onSelected,
    required this.loadPreview,
    this.audioPlayerFactory = _createAudioPlayer,
    super.key,
  }) : assert(options.length > 0);

  final List<CoachVoiceOption> options;
  final String initialVoiceId;
  final Future<String> Function(String voiceOptionId) onSelected;
  final Future<Uint8List> Function(String voiceOptionId) loadPreview;
  final EphemeralWavAudioPlayer Function() audioPlayerFactory;

  @override
  State<CoachVoiceSelectionPage> createState() =>
      _CoachVoiceSelectionPageState();
}

class _CoachVoiceSelectionPageState extends State<CoachVoiceSelectionPage> {
  late String _selectedVoiceId;
  late final EphemeralWavAudioPlayer _audioPlayer;
  StreamSubscription<void>? _completionSubscription;
  String? _loadingVoiceId;
  String? _playingVoiceId;
  String? _saveError;
  String? _previewError;
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    _selectedVoiceId = widget.initialVoiceId;
    _audioPlayer = widget.audioPlayerFactory();
    _completionSubscription = _audioPlayer.onComplete.listen((_) {
      if (!mounted) return;
      setState(() => _playingVoiceId = null);
    });
  }

  @override
  void dispose() {
    _generation++;
    unawaited(_completionSubscription?.cancel());
    unawaited(_audioPlayer.dispose());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('coach-voice-selection-page'),
      backgroundColor: Colors.transparent,
      appBar: AppBar(
        backgroundColor: Colors.transparent,
        title: const Text('选择音色'),
      ),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(
            SpeakUpDesign.space20,
            SpeakUpDesign.space8,
            SpeakUpDesign.space20,
            SpeakUpDesign.space32,
          ),
          children: [
            Text(
              '点击音色即可试听并选中',
              style: SpeakUpDesign.body.copyWith(
                color: SpeakUpDesign.secondary,
              ),
            ),
            if (_saveError != null || _previewError != null) ...[
              const SizedBox(height: SpeakUpDesign.space8),
              Text(
                [_saveError, _previewError].whereType<String>().join(' '),
                key: const Key('coach-voice-selection-error'),
                style: SpeakUpDesign.body.copyWith(
                  color: Theme.of(context).colorScheme.error,
                  fontSize: 13,
                ),
              ),
            ],
            const SizedBox(height: SpeakUpDesign.space20),
            Material(
              color: SpeakUpDesign.surface,
              borderRadius: BorderRadius.circular(20),
              clipBehavior: Clip.antiAlias,
              child: Column(
                children: [
                  for (
                    var index = 0;
                    index < widget.options.length;
                    index++
                  ) ...[
                    if (index > 0) const Divider(height: 1),
                    _VoiceOptionRow(
                      option: widget.options[index],
                      selected: widget.options[index].id == _selectedVoiceId,
                      loading: widget.options[index].id == _loadingVoiceId,
                      playing: widget.options[index].id == _playingVoiceId,
                      onTap: () => _activate(widget.options[index]),
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  void _activate(CoachVoiceOption option) {
    final changed = option.id != _selectedVoiceId;
    final generation = ++_generation;
    setState(() {
      _selectedVoiceId = option.id;
      _loadingVoiceId = option.id;
      _playingVoiceId = null;
      _saveError = null;
      _previewError = null;
    });
    if (changed) {
      unawaited(_save(option.id, generation));
    }
    unawaited(_preview(option.id, generation));
  }

  Future<void> _save(String voiceOptionId, int generation) async {
    try {
      final authoritative = await widget.onSelected(voiceOptionId);
      if (!mounted || generation != _generation) return;
      setState(() {
        _selectedVoiceId = authoritative;
        _saveError = authoritative == voiceOptionId ? null : '保存失败，请稍后重试。';
      });
    } catch (_) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _selectedVoiceId = widget.initialVoiceId;
        _saveError = '保存失败，请稍后重试。';
      });
    }
  }

  Future<void> _preview(String voiceOptionId, int generation) async {
    Uint8List? bytes;
    try {
      await _audioPlayer.stop();
      bytes = await widget.loadPreview(voiceOptionId);
      if (!mounted || generation != _generation) {
        return;
      }
      await _audioPlayer.play(bytes);
      if (!mounted || generation != _generation) return;
      setState(() {
        _loadingVoiceId = null;
        _playingVoiceId = voiceOptionId;
      });
    } catch (_) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _loadingVoiceId = null;
        _playingVoiceId = null;
        _previewError = '试听暂时不可用，请稍后重试。';
      });
    } finally {
      bytes?.fillRange(0, bytes.length, 0);
    }
  }
}

class _VoiceOptionRow extends StatelessWidget {
  const _VoiceOptionRow({
    required this.option,
    required this.selected,
    required this.loading,
    required this.playing,
    required this.onTap,
  });

  final CoachVoiceOption option;
  final bool selected;
  final bool loading;
  final bool playing;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      key: Key('coach-voice-option-${option.id}'),
      selected: selected,
      minTileHeight: 76,
      contentPadding: const EdgeInsets.symmetric(
        horizontal: SpeakUpDesign.space16,
        vertical: SpeakUpDesign.space8,
      ),
      leading: CoachVoiceGazeAvatar(
        key: Key('coach-voice-gaze-${option.id}'),
        voiceId: option.id,
      ),
      title: Row(
        children: [
          Flexible(child: Text(option.name, style: SpeakUpDesign.cardTitle)),
          const SizedBox(width: SpeakUpDesign.space4),
          CoachVoiceGenderIcon(
            key: Key('coach-voice-gender-${option.id}'),
            gender: option.gender,
          ),
        ],
      ),
      subtitle: Text(
        option.description,
        style: SpeakUpDesign.body.copyWith(fontSize: 13),
      ),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (loading)
            const SizedBox.square(
              dimension: 18,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          else if (playing)
            const Icon(
              Icons.volume_up_rounded,
              key: Key('coach-voice-preview-playing'),
              color: SpeakUpDesign.ink,
              size: 20,
            ),
          if ((loading || playing) && selected)
            const SizedBox(width: SpeakUpDesign.space8),
          if (selected)
            const Icon(
              Icons.check_circle_rounded,
              color: SpeakUpDesign.ink,
              size: 24,
            ),
        ],
      ),
      onTap: onTap,
    );
  }
}

EphemeralWavAudioPlayer _createAudioPlayer() =>
    AudioplayersEphemeralWavAudioPlayer();
