import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/features/profile/coach_voice_gaze_avatar.dart';

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
    super.key,
  }) : assert(options.length > 0);

  final List<CoachVoiceOption> options;
  final String initialVoiceId;

  @override
  State<CoachVoiceSelectionPage> createState() =>
      _CoachVoiceSelectionPageState();
}

class _CoachVoiceSelectionPageState extends State<CoachVoiceSelectionPage> {
  late String _selectedVoiceId;

  @override
  void initState() {
    super.initState();
    _selectedVoiceId = widget.initialVoiceId;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('coach-voice-selection-page'),
      backgroundColor: Colors.transparent,
      appBar: AppBar(
        backgroundColor: Colors.transparent,
        title: const Text('选择音色'),
        actions: [
          TextButton(
            key: const Key('coach-voice-selection-complete'),
            onPressed: () => Navigator.of(context).pop(_selectedVoiceId),
            child: const Text('完成'),
          ),
          const SizedBox(width: SpeakUpDesign.space8),
        ],
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
              '选择你喜欢的陪练声音',
              style: SpeakUpDesign.body.copyWith(
                color: SpeakUpDesign.secondary,
              ),
            ),
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
                      onTap: () {
                        if (_selectedVoiceId == widget.options[index].id) {
                          return;
                        }
                        setState(() {
                          _selectedVoiceId = widget.options[index].id;
                        });
                      },
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
}

class _VoiceOptionRow extends StatelessWidget {
  const _VoiceOptionRow({
    required this.option,
    required this.selected,
    required this.onTap,
  });

  final CoachVoiceOption option;
  final bool selected;
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
      trailing: SizedBox.square(
        dimension: 24,
        child: selected
            ? const Icon(
                Icons.check_circle_rounded,
                color: SpeakUpDesign.ink,
                size: 24,
              )
            : null,
      ),
      onTap: onTap,
    );
  }
}
