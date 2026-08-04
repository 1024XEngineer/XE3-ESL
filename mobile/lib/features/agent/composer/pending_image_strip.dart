import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/features/agent/composer/image/agent_image_client.dart';

typedef AgentComposerPendingImageAction = FutureOr<void> Function(String);

class AgentPendingImageStrip extends StatelessWidget {
  const AgentPendingImageStrip({
    required this.images,
    required this.onRemove,
    required this.onRetry,
    super.key,
  });

  final List<AgentPendingImage> images;
  final AgentComposerPendingImageAction? onRemove;
  final AgentComposerPendingImageAction? onRetry;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: 82,
      child: ListView.separated(
        key: const Key('agent-pending-images'),
        scrollDirection: Axis.horizontal,
        itemCount: images.length,
        separatorBuilder: (_, _) => const SizedBox(width: 8),
        itemBuilder: (context, index) {
          final pending = images[index];
          return Stack(
            children: [
              ClipRRect(
                borderRadius: BorderRadius.circular(12),
                child: Image.memory(
                  pending.image.bytes,
                  key: Key('agent-pending-image-${pending.localId}'),
                  width: 82,
                  height: 82,
                  fit: BoxFit.cover,
                  gaplessPlayback: true,
                ),
              ),
              Positioned.fill(
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    color: pending.state == AgentPendingImageState.ready
                        ? Colors.transparent
                        : const Color(0x66000000),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: switch (pending.state) {
                    AgentPendingImageState.uploading => const Center(
                      child: SizedBox.square(
                        dimension: 22,
                        child: CircularProgressIndicator(
                          strokeWidth: 2.5,
                          color: Colors.white,
                        ),
                      ),
                    ),
                    AgentPendingImageState.failed => Center(
                      child: IconButton.filled(
                        key: Key('agent-retry-image-${pending.localId}'),
                        tooltip: '重试上传',
                        onPressed: onRetry == null
                            ? null
                            : () => onRetry!(pending.localId),
                        icon: const Icon(Icons.refresh_rounded, size: 18),
                      ),
                    ),
                    AgentPendingImageState.ready => const SizedBox.shrink(),
                  },
                ),
              ),
              Positioned(
                right: 2,
                top: 2,
                child: IconButton.filled(
                  key: Key('agent-remove-image-${pending.localId}'),
                  tooltip: '移除图片',
                  onPressed: onRemove == null
                      ? null
                      : () => onRemove!(pending.localId),
                  constraints: const BoxConstraints.tightFor(
                    width: 28,
                    height: 28,
                  ),
                  padding: EdgeInsets.zero,
                  style: IconButton.styleFrom(
                    backgroundColor: const Color(0x99000000),
                    foregroundColor: Colors.white,
                  ),
                  icon: const Icon(Icons.close_rounded, size: 16),
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}
