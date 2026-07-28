import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';

class PracticeRecordingsCard extends StatelessWidget {
  const PracticeRecordingsCard({
    required this.controller,
    this.title = '本次录音',
    super.key,
  });

  final AgentController controller;
  final String title;

  @override
  Widget build(BuildContext context) {
    final recordings = controller.recordings;
    if (recordings.isEmpty) {
      return const SizedBox.shrink();
    }
    return Card(
      key: const Key('practice-recordings-card'),
      elevation: 0,
      color: Colors.white,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(18, 16, 10, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.only(left: 2, right: 8, bottom: 6),
              child: Text(
                title,
                style: const TextStyle(fontWeight: FontWeight.w700),
              ),
            ),
            for (final recording in recordings)
              _RecordingRow(
                controller: controller,
                audioAssetId: recording.audioAssetId,
                effectiveTurn: recording.effectiveTurn,
              ),
          ],
        ),
      ),
    );
  }
}

class _RecordingRow extends StatelessWidget {
  const _RecordingRow({
    required this.controller,
    required this.audioAssetId,
    required this.effectiveTurn,
  });

  final AgentController controller;
  final String audioAssetId;
  final int effectiveTurn;

  @override
  Widget build(BuildContext context) {
    final loading = controller.isRecordingAudioLoading(audioAssetId);
    final playing = controller.isRecordingAudioPlaying(audioAssetId);
    final deleting = controller.isRecordingDeleting(audioAssetId);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 2),
      child: Row(
        children: [
          Expanded(
            child: Text(
              '第 $effectiveTurn 轮录音',
              key: Key('practice-recording-label-$audioAssetId'),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          IconButton(
            key: Key('practice-recording-play-$audioAssetId'),
            tooltip: playing ? '停止播放' : '播放录音',
            onPressed: deleting || !controller.canUsePracticeAudio
                ? null
                : () => controller.toggleRecordingAudio(audioAssetId),
            icon: loading
                ? const SizedBox.square(
                    dimension: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Icon(
                    playing
                        ? Icons.stop_circle_outlined
                        : Icons.play_circle_outline_rounded,
                  ),
          ),
          IconButton(
            key: Key('practice-recording-delete-$audioAssetId'),
            tooltip: '删除录音',
            onPressed: loading || deleting
                ? null
                : () => controller.deleteRecording(audioAssetId),
            icon: deleting
                ? const SizedBox.square(
                    dimension: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.delete_outline_rounded),
          ),
        ],
      ),
    );
  }
}
