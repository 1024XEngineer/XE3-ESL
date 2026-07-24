/// Preparation module boundary.
library;

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';

class PreparationPage extends StatefulWidget {
  const PreparationPage({
    this.showBackButton = false,
    this.agentController,
    this.onSceneSelected,
    super.key,
  });

  final bool showBackButton;
  final AgentController? agentController;
  final VoidCallback? onSceneSelected;

  @override
  State<PreparationPage> createState() => _PreparationPageState();
}

class _PreparationPageState extends State<PreparationPage> {
  @override
  void initState() {
    super.initState();
    widget.agentController?.addListener(_rebuild);
  }

  @override
  void didUpdateWidget(covariant PreparationPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.agentController == widget.agentController) {
      return;
    }
    oldWidget.agentController?.removeListener(_rebuild);
    widget.agentController?.addListener(_rebuild);
  }

  @override
  void dispose() {
    widget.agentController?.removeListener(_rebuild);
    super.dispose();
  }

  void _rebuild() {
    if (mounted) {
      setState(() {});
    }
  }

  Future<void> _selectScene(AgentScene scene) async {
    final controller = widget.agentController;
    if (controller == null || controller.isBusy) {
      return;
    }
    await controller.selectScene(scene);
    if (!mounted || controller.scene?.id != scene.id) {
      return;
    }
    if (widget.onSceneSelected case final callback?) {
      callback();
    } else if (widget.showBackButton) {
      Navigator.of(context).maybePop();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('scenes-page'),
      backgroundColor: const Color(0xFFF3F3F0),
      appBar: widget.showBackButton
          ? AppBar(
              backgroundColor: const Color(0xFFF3F3F0),
              surfaceTintColor: Colors.transparent,
              elevation: 0,
              scrolledUnderElevation: 0,
              leading: IconButton(
                key: const Key('preparation-route-back-button'),
                tooltip: '返回',
                onPressed: () => Navigator.of(context).maybePop(),
                icon: const Icon(Icons.arrow_back_rounded),
              ),
            )
          : null,
      body: SafeArea(
        bottom: false,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 28, 20, 140),
          children: [
            const Text(
              '场景',
              style: TextStyle(fontSize: 32, fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 8),
            const Text(
              '直接进入已经开放的练习；未实现的场景不会提前展示。',
              style: TextStyle(color: Color(0xFF696B73), fontSize: 15),
            ),
            const SizedBox(height: 28),
            for (final scene in agentScenes) ...[
              _SceneCard(
                scene: scene,
                selected: widget.agentController?.scene?.id == scene.id,
                enabled:
                    widget.agentController != null &&
                    !(widget.agentController?.isBusy ?? false),
                onPressed: () => _selectScene(scene),
              ),
              const SizedBox(height: 12),
            ],
            if (widget.agentController?.isBusy ?? false)
              const LinearProgressIndicator(
                key: Key('scene-selection-progress'),
                minHeight: 2,
              ),
          ],
        ),
      ),
    );
  }
}

class _SceneCard extends StatelessWidget {
  const _SceneCard({
    required this.scene,
    required this.selected,
    required this.enabled,
    required this.onPressed,
  });

  final AgentScene scene;
  final bool selected;
  final bool enabled;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return Card(
      elevation: 0,
      color: Colors.white,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(20),
        side: BorderSide(
          color: selected ? const Color(0xFF303136) : const Color(0xFFE8E8E4),
          width: selected ? 1.5 : 1,
        ),
      ),
      child: InkWell(
        key: Key('scene-${scene.id}'),
        onTap: enabled ? onPressed : null,
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                width: 48,
                height: 48,
                decoration: BoxDecoration(
                  color: const Color(0xFFE8E8E5),
                  borderRadius: BorderRadius.circular(15),
                ),
                child: Icon(
                  selected ? Icons.check_rounded : Icons.work_outline_rounded,
                  color: const Color(0xFF4F5054),
                ),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      scene.title,
                      style: const TextStyle(
                        fontSize: 17,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 5),
                    Text(
                      scene.description,
                      style: const TextStyle(
                        color: Color(0xFF696B73),
                        height: 1.4,
                      ),
                    ),
                  ],
                ),
              ),
              const Icon(Icons.arrow_forward_ios_rounded, size: 16),
            ],
          ),
        ),
      ),
    );
  }
}
