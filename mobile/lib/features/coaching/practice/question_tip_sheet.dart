import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';

class QuestionTipCard extends StatefulWidget {
  const QuestionTipCard({
    required this.content,
    required this.onClose,
    required this.onSpeak,
    super.key,
  });

  final String content;
  final VoidCallback onClose;
  final Future<void> Function() onSpeak;

  @override
  State<QuestionTipCard> createState() => _QuestionTipCardState();
}

class _QuestionTipCardState extends State<QuestionTipCard> {
  bool _speaking = false;
  String? _speechError;

  Future<void> _speak() async {
    if (_speaking) {
      return;
    }
    setState(() {
      _speaking = true;
      _speechError = null;
    });
    try {
      await widget.onSpeak();
    } catch (_) {
      if (mounted) {
        setState(() => _speechError = '暂时无法朗读，请稍后重试。');
      }
    } finally {
      if (mounted) {
        setState(() => _speaking = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('practice-question-tip-card'),
      margin: const EdgeInsets.fromLTRB(16, 8, 16, 8),
      padding: const EdgeInsets.fromLTRB(14, 10, 10, 10),
      decoration: BoxDecoration(
        color: SpeakUpDesign.primaryMuted,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: const Color(0xFFC9DEE3)),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              const Icon(
                Icons.lightbulb_outline_rounded,
                size: 18,
                color: SpeakUpDesign.primary,
              ),
              const SizedBox(width: 6),
              const Expanded(
                child: Text('参考回答', style: SpeakUpDesign.cardTitle),
              ),
              IconButton(
                tooltip: '收起参考回答',
                onPressed: widget.onClose,
                visualDensity: VisualDensity.compact,
                constraints: const BoxConstraints.tightFor(
                  width: 34,
                  height: 34,
                ),
                icon: const Icon(Icons.close_rounded, size: 20),
              ),
            ],
          ),
          ConstrainedBox(
            constraints: const BoxConstraints(maxHeight: 112),
            child: SingleChildScrollView(
              child: Text(widget.content, style: SpeakUpDesign.body),
            ),
          ),
          const SizedBox(height: 6),
          Row(
            children: [
              TextButton.icon(
                key: const Key('practice-question-tip-speak-inline'),
                onPressed: _speaking ? null : _speak,
                style: TextButton.styleFrom(
                  foregroundColor: SpeakUpDesign.primary,
                  padding: const EdgeInsets.symmetric(horizontal: 6),
                  minimumSize: const Size(0, 34),
                  tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                  visualDensity: VisualDensity.compact,
                ),
                icon: _speaking
                    ? const SizedBox.square(
                        dimension: 15,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.volume_up_outlined, size: 18),
                label: Text(_speaking ? '正在朗读' : '朗读'),
              ),
            ],
          ),
          if (_speechError case final message?) ...[
            const SizedBox(height: 4),
            Text(
              message,
              style: const TextStyle(color: SpeakUpDesign.error, fontSize: 12),
            ),
          ],
        ],
      ),
    );
  }
}

class QuestionTipSheet extends StatefulWidget {
  const QuestionTipSheet({
    required this.content,
    required this.onSpeak,
    super.key,
  });

  final String content;
  final Future<void> Function() onSpeak;

  @override
  State<QuestionTipSheet> createState() => _QuestionTipSheetState();
}

class _QuestionTipSheetState extends State<QuestionTipSheet> {
  bool _speaking = false;
  String? _speechError;

  Future<void> _speak() async {
    if (_speaking) {
      return;
    }
    setState(() {
      _speaking = true;
      _speechError = null;
    });
    try {
      await widget.onSpeak();
    } catch (_) {
      if (mounted) {
        setState(() => _speechError = '暂时无法朗读，请稍后重试。');
      }
    } finally {
      if (mounted) {
        setState(() => _speaking = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.fromLTRB(
        24,
        12,
        24,
        24 + MediaQuery.viewInsetsOf(context).bottom,
      ),
      child: Column(
        key: const Key('practice-question-tip-sheet'),
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              const Expanded(
                child: Text('参考回答', style: SpeakUpDesign.sectionTitle),
              ),
              IconButton(
                tooltip: '关闭',
                onPressed: () => Navigator.of(context).pop(),
                icon: const Icon(Icons.close_rounded),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Text(widget.content, style: SpeakUpDesign.body),
          const SizedBox(height: 18),
          OutlinedButton.icon(
            key: const Key('practice-question-tip-speak'),
            onPressed: _speaking ? null : _speak,
            icon: _speaking
                ? const SizedBox.square(
                    dimension: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.volume_up_outlined),
            label: Text(_speaking ? '正在朗读' : '朗读参考回答'),
          ),
          if (_speechError case final message?) ...[
            const SizedBox(height: 8),
            Text(
              message,
              textAlign: TextAlign.center,
              style: const TextStyle(color: SpeakUpDesign.error, fontSize: 12),
            ),
          ],
          const SizedBox(height: 8),
          const Text(
            '你可以照着念，也可以用自己的表达；此内容不会自动提交。',
            textAlign: TextAlign.center,
            style: SpeakUpDesign.meta,
          ),
        ],
      ),
    );
  }
}
