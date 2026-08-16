import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/ielts/ielts_answer_generation.dart';
import 'package:speakup/features/coaching/ielts/ielts_speech_client.dart';
import 'package:speakup/features/coaching/practice/practice_audio_player.dart';
import 'package:speakup/features/coaching/practice/practice_prompt_speaker.dart';
import 'package:speakup/features/coaching/preparation/preparation_design.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

class IeltsSetDetailPage extends StatefulWidget {
  const IeltsSetDetailPage({
    required this.mode,
    required this.title,
    required this.subtitle,
    required this.questions,
    required this.onStart,
    this.cueCard,
    this.speechClient,
    this.audioPlayer,
    this.answerGenerator,
    this.answerSpeaker,
    this.cueCardQuestionReference,
    this.questionReferences = const <IeltsQuestionReference>[],
    super.key,
  }) : assert((speechClient == null) == (audioPlayer == null));

  final PracticeMode mode;
  final String title;
  final String subtitle;
  final List<String> questions;
  final IeltsCueCard? cueCard;
  final ValueChanged<List<IeltsPreparedAnswer>>? onStart;
  final IeltsSpeechClient? speechClient;
  final PracticeAudioPlayer? audioPlayer;
  final IeltsAnswerGenerator? answerGenerator;
  final PracticePromptSpeaker? answerSpeaker;
  final IeltsQuestionReference? cueCardQuestionReference;
  final List<IeltsQuestionReference> questionReferences;

  @override
  State<IeltsSetDetailPage> createState() => _IeltsSetDetailPageState();
}

class _IeltsSetDetailPageState extends State<IeltsSetDetailPage> {
  StreamSubscription<void>? _speechCompletion;
  int? _speakingQuestionIndex;
  String? _speechError;
  int _speechRequest = 0;
  int? _expandedAnswerIndex;
  final Map<int, IeltsGeneratedAnswer> _answers = {};
  final Set<int> _personalizedAnswers = {};
  final Set<int> _generatingAnswers = {};
  final Map<int, String> _answerErrors = {};
  PracticePromptSpeaker? _ownedAnswerSpeaker;
  int? _speakingAnswerIndex;
  int _answerSpeechRequest = 0;

  String get _partLabel => switch (widget.mode) {
    PracticeMode.part1 => 'Part 1',
    PracticeMode.part2 => 'Part 2',
    PracticeMode.part3 => 'Part 3',
    _ => throw StateError('IELTS set details require a section mode.'),
  };

  String get _questionsTitle => switch (widget.mode) {
    PracticeMode.part2 => '同组 Part 3 · ${widget.questions.length} 题',
    _ => '$_partLabel · ${widget.questions.length} 题',
  };

  @override
  void initState() {
    super.initState();
    _speechCompletion = widget.audioPlayer?.onComplete.listen((_) {
      if (mounted && _speakingQuestionIndex != null) {
        setState(() => _speakingQuestionIndex = null);
      }
    });
  }

  @override
  void dispose() {
    _speechRequest++;
    _answerSpeechRequest++;
    unawaited(_speechCompletion?.cancel());
    unawaited(widget.audioPlayer?.stop());
    if (widget.answerSpeaker case final speaker?) {
      unawaited(speaker.stop());
    } else if (_ownedAnswerSpeaker case final speaker?) {
      unawaited(speaker.dispose());
    }
    super.dispose();
  }

  Future<void> _toggleQuestionSpeech(int index) async {
    await _stopAnswerSpeech();
    final reference = widget.questionReferences[index];
    await _toggleSpeech(
      questionIndex: index,
      load: () => widget.speechClient!.loadQuestion(reference),
      failureMessage: '题目朗读失败，请稍后重试。',
    );
  }

  Future<void> _generateAnswer(
    int index,
    IeltsQuestionReference question, {
    required bool personalized,
  }) async {
    if (_generatingAnswers.contains(index) || widget.answerGenerator == null) {
      return;
    }
    final points = personalized
        ? await showModalBottomSheet<List<String>>(
            context: context,
            useSafeArea: true,
            isScrollControlled: true,
            builder: (_) => const _PersonalAnswerSheet(),
          )
        : const <String>[];
    if (points == null || !mounted) return;
    setState(() {
      _generatingAnswers.add(index);
      _answerErrors.remove(index);
    });
    try {
      final answer = await widget.answerGenerator!.generate(
        question: question,
        personalPoints: points,
      );
      if (mounted) {
        setState(() {
          _answers[index] = answer;
          if (personalized) {
            _personalizedAnswers.add(index);
          } else {
            _personalizedAnswers.remove(index);
          }
        });
      }
    } on Object {
      if (mounted) {
        setState(() => _answerErrors[index] = '这次没有生成成功，请重试。');
      }
    } finally {
      if (mounted) setState(() => _generatingAnswers.remove(index));
    }
  }

  Future<void> _toggleAnswerSpeech(
    int index,
    IeltsGeneratedAnswer answer,
  ) async {
    final request = ++_answerSpeechRequest;
    final wasSpeaking = _speakingAnswerIndex == index;
    try {
      await widget.audioPlayer?.stop();
      if (mounted) setState(() => _speakingQuestionIndex = null);
      final speaker =
          widget.answerSpeaker ??
          (_ownedAnswerSpeaker ??= SystemPracticePromptSpeaker());
      await speaker.stop();
      if (!mounted || request != _answerSpeechRequest) return;
      if (wasSpeaking) {
        setState(() => _speakingAnswerIndex = null);
        return;
      }
      setState(() {
        _speakingAnswerIndex = index;
        _answerErrors.remove(index);
      });
      await speaker.speak(answer.speechText);
    } on Object {
      if (mounted && request == _answerSpeechRequest) {
        setState(() => _answerErrors[index] = '回答播放失败，请重试。');
      }
    } finally {
      if (mounted && request == _answerSpeechRequest) {
        setState(() => _speakingAnswerIndex = null);
      }
    }
  }

  Future<void> _stopAnswerSpeech() async {
    _answerSpeechRequest++;
    await (widget.answerSpeaker ?? _ownedAnswerSpeaker)?.stop();
    if (mounted && _speakingAnswerIndex != null) {
      setState(() => _speakingAnswerIndex = null);
    }
  }

  List<IeltsPreparedAnswer> _preparedAnswers() {
    final result = <IeltsPreparedAnswer>[];
    for (final entry in _answers.entries) {
      final reference = widget.mode == PracticeMode.part2
          ? widget.cueCardQuestionReference
          : entry.key < widget.questionReferences.length
          ? widget.questionReferences[entry.key]
          : null;
      if (reference == null) continue;
      result.add(
        IeltsPreparedAnswer(
          bankId: reference.bankId,
          part: reference.part,
          sourceId: reference.sourceId,
          questionPosition: reference.questionPosition,
          answer: entry.value.answer.trim(),
          personalized: _personalizedAnswers.contains(entry.key),
        ),
      );
    }
    result.sort(
      (left, right) => left.questionPosition.compareTo(right.questionPosition),
    );
    return List<IeltsPreparedAnswer>.unmodifiable(result);
  }

  Future<void> _toggleSpeech({
    int? questionIndex,
    required Future<Uint8List> Function() load,
    required String failureMessage,
  }) async {
    final request = ++_speechRequest;
    final wasSpeaking = _speakingQuestionIndex == questionIndex;

    try {
      await widget.audioPlayer!.stop();
      if (!mounted || request != _speechRequest) {
        return;
      }
      if (wasSpeaking) {
        setState(() {
          _speakingQuestionIndex = null;
          _speechError = null;
        });
        return;
      }

      setState(() {
        _speakingQuestionIndex = questionIndex;
        _speechError = null;
      });
      final bytes = await load();
      try {
        if (!mounted || request != _speechRequest) {
          return;
        }
        await widget.audioPlayer!.playWav(bytes);
      } finally {
        bytes.fillRange(0, bytes.length, 0);
      }
    } catch (_) {
      if (mounted && request == _speechRequest) {
        setState(() {
          _speakingQuestionIndex = null;
          _speechError = failureMessage;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('ielts-set-detail'),
      backgroundColor: PreparationDesign.canvas,
      appBar: AppBar(
        backgroundColor: PreparationDesign.canvas,
        surfaceTintColor: Colors.transparent,
        leading: IconButton(
          key: const Key('ielts-set-detail-back'),
          tooltip: '返回题库',
          onPressed: () => Navigator.of(context).pop(),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
        title: Text(
          'IELTS · $_partLabel',
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
      ),
      body: SafeArea(
        top: false,
        child: Column(
          children: [
            Expanded(
              child: SingleChildScrollView(
                key: const Key('ielts-set-detail-scroll'),
                padding: PreparationDesign.pagePadding(
                  context,
                  hasPrimaryNavigation: false,
                  top: 12,
                ).copyWith(bottom: 24),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (widget.mode == PracticeMode.part1)
                      _PartOneHeader(
                        title: widget.title,
                        subtitle: widget.subtitle,
                        questionCount: widget.questions.length,
                      )
                    else ...[
                      _PartBadge(label: _partLabel),
                      const SizedBox(height: 14),
                      Text(
                        widget.title,
                        key: const Key('ielts-set-detail-title'),
                        style: PreparationDesign.pageTitle,
                      ),
                      if (widget.subtitle.isNotEmpty) ...[
                        const SizedBox(height: 8),
                        Text(
                          widget.subtitle,
                          key: const Key('ielts-set-detail-subtitle'),
                          style: PreparationDesign.body,
                        ),
                      ],
                      const SizedBox(height: 28),
                    ],
                    if (widget.cueCard case final value?) ...[
                      _CueCardContent(cueCard: value),
                      if (widget.answerGenerator != null &&
                          widget.cueCardQuestionReference != null)
                        _AnswerPreparationPanel(
                          index: 0,
                          expanded: _expandedAnswerIndex == 0,
                          generating: _generatingAnswers.contains(0),
                          answer: _answers[0],
                          personalized: _personalizedAnswers.contains(0),
                          speaking: _speakingAnswerIndex == 0,
                          errorMessage: _answerErrors[0],
                          onToggle: () => setState(
                            () => _expandedAnswerIndex =
                                _expandedAnswerIndex == 0 ? null : 0,
                          ),
                          onExample: () => _generateAnswer(
                            0,
                            widget.cueCardQuestionReference!,
                            personalized: false,
                          ),
                          onPersonalize: () => _generateAnswer(
                            0,
                            widget.cueCardQuestionReference!,
                            personalized: true,
                          ),
                          onSpeak: _answers[0] == null
                              ? null
                              : () => _toggleAnswerSpeech(0, _answers[0]!),
                        ),
                      if (widget.questions.isNotEmpty)
                        const SizedBox(height: 30),
                    ],
                    if (widget.questions.isNotEmpty) ...[
                      if (widget.mode != PracticeMode.part1) ...[
                        Text(
                          _questionsTitle,
                          style: PreparationDesign.sectionTitle,
                        ),
                        const SizedBox(height: 8),
                      ],
                      for (
                        var index = 0;
                        index < widget.questions.length;
                        index++
                      ) ...[
                        _QuestionRow(
                          index: index + 1,
                          question: widget.questions[index],
                          speaking: _speakingQuestionIndex == index,
                          onSpeak:
                              widget.speechClient == null ||
                                  index >= widget.questionReferences.length
                              ? null
                              : () => _toggleQuestionSpeech(index),
                        ),
                        if (widget.answerGenerator != null &&
                            index < widget.questionReferences.length)
                          _AnswerPreparationPanel(
                            index: index,
                            expanded: _expandedAnswerIndex == index,
                            generating: _generatingAnswers.contains(index),
                            answer: _answers[index],
                            personalized: _personalizedAnswers.contains(index),
                            speaking: _speakingAnswerIndex == index,
                            errorMessage: _answerErrors[index],
                            onToggle: () => setState(
                              () => _expandedAnswerIndex =
                                  _expandedAnswerIndex == index ? null : index,
                            ),
                            onExample: () => _generateAnswer(
                              index,
                              widget.questionReferences[index],
                              personalized: false,
                            ),
                            onPersonalize: () => _generateAnswer(
                              index,
                              widget.questionReferences[index],
                              personalized: true,
                            ),
                            onSpeak: _answers[index] == null
                                ? null
                                : () => _toggleAnswerSpeech(
                                    index,
                                    _answers[index]!,
                                  ),
                          ),
                      ],
                      if (_speechError case final message?) ...[
                        const SizedBox(height: 12),
                        Text(
                          message,
                          key: const Key('ielts-set-detail-speech-error'),
                          style: PreparationDesign.body.copyWith(
                            color: Theme.of(context).colorScheme.error,
                          ),
                        ),
                      ],
                    ],
                  ],
                ),
              ),
            ),
            DecoratedBox(
              decoration: const BoxDecoration(
                color: PreparationDesign.surface,
                border: Border(
                  top: BorderSide(color: PreparationDesign.border),
                ),
              ),
              child: Padding(
                padding: EdgeInsets.fromLTRB(
                  PreparationDesign.horizontalInset(context),
                  12,
                  PreparationDesign.horizontalInset(context),
                  12 + MediaQuery.viewPaddingOf(context).bottom,
                ),
                child: FilledButton(
                  key: const Key('ielts-set-detail-start'),
                  onPressed: widget.onStart == null
                      ? null
                      : () => widget.onStart!(_preparedAnswers()),
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(52),
                    backgroundColor: PreparationDesign.ink,
                    foregroundColor: Colors.white,
                    textStyle: PreparationDesign.cardTitle,
                  ),
                  child: Text(
                    widget.onStart == null
                        ? '练习暂不可用'
                        : widget.mode == PracticeMode.part1
                        ? '开始整组练习'
                        : '开始 $_partLabel',
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _PartOneHeader extends StatelessWidget {
  const _PartOneHeader({
    required this.title,
    required this.subtitle,
    required this.questionCount,
  });

  final String title;
  final String subtitle;
  final int questionCount;

  @override
  Widget build(BuildContext context) => Column(
    key: const Key('ielts-set-detail-topic'),
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Wrap(
        crossAxisAlignment: WrapCrossAlignment.end,
        spacing: 14,
        runSpacing: 6,
        children: [
          Text(
            title,
            key: const Key('ielts-set-detail-title'),
            style: PreparationDesign.pageTitle,
          ),
          if (subtitle.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 2),
              child: Text(
                subtitle,
                key: const Key('ielts-set-detail-subtitle'),
                style: PreparationDesign.sectionTitle.copyWith(
                  color: PreparationDesign.inkSecondary,
                  fontWeight: FontWeight.w500,
                ),
              ),
            ),
        ],
      ),
      const SizedBox(height: 8),
      Text(
        '$questionCount 题',
        style: PreparationDesign.body.copyWith(
          color: PreparationDesign.inkSecondary,
        ),
      ),
      const SizedBox(height: 20),
      const Divider(height: 1, color: PreparationDesign.border),
      const SizedBox(height: 8),
    ],
  );
}

class _PartBadge extends StatelessWidget {
  const _PartBadge({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) => DecoratedBox(
    decoration: BoxDecoration(
      color: PreparationDesign.surfaceMuted,
      borderRadius: BorderRadius.circular(PreparationDesign.radiusControl),
    ),
    child: Padding(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      child: Text(label.toUpperCase(), style: PreparationDesign.label),
    ),
  );
}

class _CueCardContent extends StatelessWidget {
  const _CueCardContent({required this.cueCard});

  final IeltsCueCard cueCard;

  @override
  Widget build(BuildContext context) => Column(
    key: const Key('ielts-set-detail-cue-card'),
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      const Text('Cue Card', style: PreparationDesign.sectionTitle),
      const SizedBox(height: 10),
      Text(
        cueCard.prompt,
        style: PreparationDesign.cardTitle.copyWith(fontSize: 18),
      ),
      if (cueCard.points.isNotEmpty) ...[
        const SizedBox(height: 14),
        const Text('You should say:', style: PreparationDesign.label),
        const SizedBox(height: 6),
        for (final point in cueCard.points)
          Padding(
            padding: const EdgeInsets.only(bottom: 6),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Padding(
                  padding: EdgeInsets.only(top: 7),
                  child: Icon(Icons.circle, size: 5),
                ),
                const SizedBox(width: 10),
                Expanded(child: Text(point, style: PreparationDesign.body)),
              ],
            ),
          ),
      ],
    ],
  );
}

class _QuestionRow extends StatelessWidget {
  const _QuestionRow({
    required this.index,
    required this.question,
    required this.speaking,
    required this.onSpeak,
  });

  final int index;
  final String question;
  final bool speaking;
  final VoidCallback? onSpeak;

  @override
  Widget build(BuildContext context) => Container(
    key: Key('ielts-set-detail-question-$index'),
    width: double.infinity,
    padding: const EdgeInsets.symmetric(vertical: 12),
    decoration: const BoxDecoration(
      border: Border(bottom: BorderSide(color: PreparationDesign.border)),
    ),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        SizedBox(
          width: 28,
          child: Text(
            '$index',
            style: PreparationDesign.label.copyWith(
              color: PreparationDesign.inkSecondary,
            ),
          ),
        ),
        Expanded(
          child: Text(
            question,
            style: PreparationDesign.body.copyWith(
              color: PreparationDesign.ink,
            ),
          ),
        ),
        const SizedBox(width: 10),
        IconButton.outlined(
          key: Key('ielts-set-detail-speak-$index'),
          tooltip: speaking ? '停止播放第 $index 题' : '听考官读第 $index 题',
          onPressed: onSpeak,
          icon: Icon(
            speaking ? Icons.stop_rounded : Icons.volume_up_outlined,
            size: 22,
          ),
          style: IconButton.styleFrom(
            minimumSize: const Size.square(44),
            side: const BorderSide(color: PreparationDesign.border),
          ),
        ),
      ],
    ),
  );
}

class _AnswerPreparationPanel extends StatelessWidget {
  const _AnswerPreparationPanel({
    required this.index,
    required this.expanded,
    required this.generating,
    required this.answer,
    required this.personalized,
    required this.speaking,
    required this.errorMessage,
    required this.onToggle,
    required this.onExample,
    required this.onPersonalize,
    required this.onSpeak,
  });

  final int index;
  final bool expanded;
  final bool generating;
  final IeltsGeneratedAnswer? answer;
  final bool personalized;
  final bool speaking;
  final String? errorMessage;
  final VoidCallback onToggle;
  final VoidCallback onExample;
  final VoidCallback onPersonalize;
  final VoidCallback? onSpeak;

  @override
  Widget build(BuildContext context) => Container(
    key: Key('ielts-answer-panel-${index + 1}'),
    width: double.infinity,
    decoration: const BoxDecoration(
      border: Border(bottom: BorderSide(color: PreparationDesign.border)),
    ),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        TextButton.icon(
          key: Key('ielts-answer-toggle-${index + 1}'),
          onPressed: onToggle,
          icon: Icon(
            expanded
                ? Icons.keyboard_arrow_up_rounded
                : Icons.lightbulb_outline_rounded,
          ),
          label: Text(expanded ? '收起答案准备' : '准备这道题的答案'),
          style: TextButton.styleFrom(
            alignment: Alignment.centerLeft,
            minimumSize: const Size.fromHeight(44),
            foregroundColor: PreparationDesign.ink,
          ),
        ),
        if (expanded)
          Padding(
            padding: const EdgeInsets.only(bottom: 16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (generating)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 16),
                    child: Row(
                      children: [
                        SizedBox.square(
                          dimension: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        ),
                        SizedBox(width: 10),
                        Text('正在准备表达…'),
                      ],
                    ),
                  )
                else ...[
                  if (answer case final value?) ...[
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            personalized ? '我的回答' : '参考回答',
                            style: PreparationDesign.cardTitle,
                          ),
                        ),
                        IconButton(
                          key: Key('ielts-answer-speak-${index + 1}'),
                          tooltip: speaking ? '停止播放回答' : '播放回答',
                          onPressed: onSpeak,
                          icon: Icon(
                            speaking
                                ? Icons.stop_rounded
                                : Icons.volume_up_outlined,
                          ),
                        ),
                        TextButton.icon(
                          key: Key('ielts-answer-adjust-${index + 1}'),
                          onPressed: onPersonalize,
                          icon: const Icon(Icons.auto_awesome_outlined),
                          label: Text(personalized ? '调整' : '定制'),
                        ),
                      ],
                    ),
                    const SizedBox(height: 4),
                    Text(
                      value.answer,
                      key: Key('ielts-answer-body-${index + 1}'),
                      style: PreparationDesign.body,
                    ),
                    if (value.outline.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Text(
                        '回答思路：${value.outline.join(' → ')}',
                        style: PreparationDesign.label.copyWith(
                          color: PreparationDesign.inkSecondary,
                        ),
                      ),
                    ],
                  ] else
                    Row(
                      children: [
                        Expanded(
                          child: OutlinedButton(
                            onPressed: onExample,
                            child: const Text('生成示例回答'),
                          ),
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: FilledButton(
                            key: Key('ielts-personalize-answer-${index + 1}'),
                            onPressed: onPersonalize,
                            style: FilledButton.styleFrom(
                              backgroundColor: PreparationDesign.ink,
                              foregroundColor: Colors.white,
                            ),
                            child: const Text('定制我的答案'),
                          ),
                        ),
                      ],
                    ),
                ],
                if (errorMessage case final message?) ...[
                  const SizedBox(height: 10),
                  Text(
                    message,
                    style: PreparationDesign.body.copyWith(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ],
              ],
            ),
          ),
      ],
    ),
  );
}

class _PersonalAnswerSheet extends StatefulWidget {
  const _PersonalAnswerSheet();

  @override
  State<_PersonalAnswerSheet> createState() => _PersonalAnswerSheetState();
}

class _PersonalAnswerSheetState extends State<_PersonalAnswerSheet> {
  final _controller = TextEditingController();
  bool _showError = false;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _submit() {
    final points = _controller.text
        .split('\n')
        .map((value) => value.trim())
        .where((value) => value.isNotEmpty)
        .take(12)
        .toList(growable: false);
    if (points.isEmpty) {
      setState(() => _showError = true);
      return;
    }
    Navigator.of(context).pop(points);
  }

  @override
  Widget build(BuildContext context) => Padding(
    padding: EdgeInsets.fromLTRB(
      24,
      20,
      24,
      20 + MediaQuery.viewInsetsOf(context).bottom,
    ),
    child: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Text('定制我的答案', style: PreparationDesign.sectionTitle),
        const SizedBox(height: 8),
        const Text('写下你的真实经历或观点，每行一个要点。'),
        const SizedBox(height: 14),
        TextField(
          key: const Key('ielts-personal-answer-points'),
          controller: _controller,
          autofocus: true,
          minLines: 3,
          maxLines: 6,
          maxLength: 1500,
          decoration: InputDecoration(
            hintText: '例如：我通勤时喜欢听轻快的音乐',
            errorText: _showError ? '请先写一条真实经历或观点。' : null,
            border: const OutlineInputBorder(),
          ),
        ),
        FilledButton(
          key: const Key('ielts-generate-personal-answer'),
          onPressed: _submit,
          style: FilledButton.styleFrom(
            minimumSize: const Size.fromHeight(50),
            backgroundColor: PreparationDesign.ink,
            foregroundColor: Colors.white,
          ),
          child: const Text('生成我的表达'),
        ),
      ],
    ),
  );
}
