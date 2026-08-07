import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';

final class MessageTranslationDisclosure extends StatefulWidget {
  const MessageTranslationDisclosure({
    required this.sourceId,
    required this.buttonKey,
    required this.contentKey,
    required this.errorKey,
    this.onTranslate,
    this.leadingActions = const <Widget>[],
    super.key,
  });

  final String sourceId;
  final Key buttonKey;
  final Key contentKey;
  final Key errorKey;
  final Future<String> Function()? onTranslate;
  final List<Widget> leadingActions;

  @override
  State<MessageTranslationDisclosure> createState() =>
      _MessageTranslationDisclosureState();
}

final class _MessageTranslationDisclosureState
    extends State<MessageTranslationDisclosure> {
  String? _translation;
  bool _expanded = false;
  bool _loading = false;
  bool _failed = false;

  @override
  void didUpdateWidget(covariant MessageTranslationDisclosure oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.sourceId != widget.sourceId) {
      _translation = null;
      _expanded = false;
      _loading = false;
      _failed = false;
    }
  }

  Future<void> _toggle() async {
    if (_expanded) {
      setState(() => _expanded = false);
      return;
    }
    if (_translation != null) {
      setState(() => _expanded = true);
      return;
    }
    final translate = widget.onTranslate;
    if (translate == null || _loading) {
      return;
    }
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final translation = (await translate()).trim();
      if (!mounted) {
        return;
      }
      if (translation.isEmpty) {
        throw const FormatException('translation is empty');
      }
      setState(() {
        _translation = translation;
        _expanded = true;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) {
        return;
      }
      setState(() {
        _loading = false;
        _failed = true;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final hasActions =
        widget.leadingActions.isNotEmpty || widget.onTranslate != null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (_expanded && _translation != null) ...[
          const SizedBox(height: 8),
          Text(
            _translation!,
            key: widget.contentKey,
            style: const TextStyle(
              color: SpeakUpDesign.secondary,
              fontSize: 14,
              height: 1.45,
            ),
          ),
        ],
        if (_failed) ...[
          const SizedBox(height: 6),
          Text(
            '翻译失败，请重试。',
            key: widget.errorKey,
            style: const TextStyle(
              color: SpeakUpDesign.error,
              fontSize: 12,
              height: 1.35,
            ),
          ),
        ],
        if (hasActions) ...[
          const SizedBox(height: 6),
          Wrap(
            spacing: 4,
            runSpacing: 4,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              ...widget.leadingActions,
              if (widget.onTranslate != null)
                Semantics(
                  label: _semanticLabel,
                  button: true,
                  child: IconButton(
                    key: widget.buttonKey,
                    tooltip: _semanticLabel,
                    onPressed: _loading ? null : _toggle,
                    visualDensity: VisualDensity.compact,
                    constraints: const BoxConstraints(
                      minWidth: 36,
                      minHeight: 36,
                    ),
                    icon: _loading
                        ? const SizedBox.square(
                            dimension: 16,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.translate_rounded, size: 19),
                  ),
                ),
            ],
          ),
        ],
      ],
    );
  }

  String get _semanticLabel {
    if (_loading) {
      return '正在翻译';
    }
    if (_expanded) {
      return '收起翻译';
    }
    if (_failed) {
      return '重试翻译';
    }
    return '翻译';
  }
}
