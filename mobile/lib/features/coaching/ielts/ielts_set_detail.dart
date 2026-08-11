import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/features/coaching/ielts/ielts_answer_preparation.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
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
    this.promptSpeaker,
    this.answerPreparationClient,
    this.cueCardQuestionReference,
    this.questionReferences = const <IeltsAnswerQuestionReference>[],
    super.key,
  });

  final PracticeMode mode;
  final String title;
  final String subtitle;
  final List<String> questions;
  final IeltsCueCard? cueCard;
  final VoidCallback? onStart;
  final PracticePromptSpeaker? promptSpeaker;
  final IeltsAnswerPreparationClient? answerPreparationClient;
  final IeltsAnswerQuestionReference? cueCardQuestionReference;
  final List<IeltsAnswerQuestionReference> questionReferences;

  @override
  State<IeltsSetDetailPage> createState() => _IeltsSetDetailPageState();
}

class _IeltsSetDetailPageState extends State<IeltsSetDetailPage> {
  late final PracticePromptSpeaker _promptSpeaker;
  late final bool _ownsPromptSpeaker;
  int? _speakingQuestionIndex;
  int? _speakingAnswerIndex;
  int? _expandedQuestionIndex;
  bool _cueCardAnswerExpanded = false;
  final Map<int, IeltsAnswerPreparation> _preparations = {};
  final Set<int> _generatingQuestionIndexes = {};
  final Map<int, String> _answerErrors = {};
  final Map<int, int> _answerRequests = {};
  String? _speechError;
  int _speechRequest = 0;

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
    _ownsPromptSpeaker = widget.promptSpeaker == null;
    _promptSpeaker = widget.promptSpeaker ?? SystemPracticePromptSpeaker();
    if (_canPrepareQuestionAnswers) {
      _expandedQuestionIndex = 0;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) {
          unawaited(_ensureExample(0, widget.questionReferences[0]));
        }
      });
    } else if (_canPrepareCueCardAnswer) {
      _cueCardAnswerExpanded = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) {
          unawaited(_ensureExample(0, widget.cueCardQuestionReference!));
        }
      });
    }
  }

  bool get _canPrepareQuestionAnswers =>
      (widget.mode == PracticeMode.part1 ||
          widget.mode == PracticeMode.part3) &&
      widget.answerPreparationClient != null &&
      widget.questions.isNotEmpty &&
      widget.questionReferences.length == widget.questions.length;

  bool get _canPrepareCueCardAnswer =>
      widget.mode == PracticeMode.part2 &&
      widget.cueCard != null &&
      widget.answerPreparationClient != null &&
      widget.cueCardQuestionReference != null;

  int _startAnswerRequest(int index) {
    final request = (_answerRequests[index] ?? 0) + 1;
    setState(() {
      _answerRequests[index] = request;
      _generatingQuestionIndexes.add(index);
      _answerErrors.remove(index);
    });
    return request;
  }

  bool _isCurrentAnswerRequest(int index, int request) =>
      mounted && _answerRequests[index] == request;

  void _finishAnswerRequest(int index, int request) {
    if (_isCurrentAnswerRequest(index, request)) {
      setState(() => _generatingQuestionIndexes.remove(index));
    }
  }

  @override
  void dispose() {
    _speechRequest++;
    if (_ownsPromptSpeaker) {
      unawaited(_promptSpeaker.dispose());
    } else {
      unawaited(_promptSpeaker.stop());
    }
    super.dispose();
  }

  Future<void> _toggleQuestionSpeech(int index) async {
    final request = ++_speechRequest;
    final wasSpeaking = _speakingQuestionIndex == index;

    try {
      await _promptSpeaker.stop();
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
        _speakingQuestionIndex = index;
        _speakingAnswerIndex = null;
        _speechError = null;
      });
      await _promptSpeaker.speak(widget.questions[index]);
      if (mounted && request == _speechRequest) {
        setState(() => _speakingQuestionIndex = null);
      }
    } catch (_) {
      if (mounted && request == _speechRequest) {
        setState(() {
          _speakingQuestionIndex = null;
          _speechError = '题目朗读失败，请稍后重试。';
        });
      }
    }
  }

  Future<void> _toggleAnswerSpeech(int index, String text) async {
    final request = ++_speechRequest;
    final wasSpeaking = _speakingAnswerIndex == index;
    try {
      await _promptSpeaker.stop();
      if (!mounted || request != _speechRequest) {
        return;
      }
      if (wasSpeaking) {
        setState(() {
          _speakingAnswerIndex = null;
          _speechError = null;
        });
        return;
      }
      setState(() {
        _speakingQuestionIndex = null;
        _speakingAnswerIndex = index;
        _speechError = null;
      });
      await _promptSpeaker.speak(text);
      if (mounted && request == _speechRequest) {
        setState(() => _speakingAnswerIndex = null);
      }
    } catch (_) {
      if (mounted && request == _speechRequest) {
        setState(() {
          _speakingAnswerIndex = null;
          _speechError = '答案朗读失败，请稍后重试。';
        });
      }
    }
  }

  Future<void> _prepareAnswer(
    int index,
    IeltsAnswerQuestionReference question,
  ) async {
    final points = await showModalBottomSheet<List<String>>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      backgroundColor: PreparationDesign.surface,
      builder: (_) => const _PolishExperienceSheet(),
    );
    if (points == null || !mounted) {
      return;
    }
    if (_generatingQuestionIndexes.contains(index)) {
      return;
    }
    final request = _startAnswerRequest(index);
    try {
      final existing = _preparations[index];
      var draft =
          existing ??
          await widget.answerPreparationClient!.create(
            question: question,
            personalPoints: points,
            targetBand: 7,
          );
      if (draft.status != IeltsAnswerPreparationStatus.draft ||
          !_samePersonalPoints(draft.personalPoints, points) ||
          draft.targetBand != 7) {
        draft = await widget.answerPreparationClient!.update(
          id: draft.id,
          expectedVersion: draft.version,
          personalPoints: points,
          targetBand: 7,
        );
      }
      final ready = await widget.answerPreparationClient!.generate(
        id: draft.id,
        expectedVersion: draft.version,
      );
      if (_isCurrentAnswerRequest(index, request)) {
        setState(() {
          _preparations[index] = ready;
          _answerErrors.remove(index);
        });
      }
    } on IeltsAnswerPreparationException catch (error) {
      if (_isCurrentAnswerRequest(index, request)) {
        setState(() => _answerErrors[index] = _answerFailureMessage(error));
      }
    } catch (_) {
      if (_isCurrentAnswerRequest(index, request)) {
        setState(() => _answerErrors[index] = '暂时无法生成答案，请稍后重试。');
      }
    } finally {
      _finishAnswerRequest(index, request);
    }
  }

  Future<void> _ensureExample(
    int index,
    IeltsAnswerQuestionReference question,
  ) async {
    if (_preparations.containsKey(index) ||
        _generatingQuestionIndexes.contains(index)) {
      return;
    }
    final request = _startAnswerRequest(index);
    try {
      final draft = await widget.answerPreparationClient!.create(
        question: question,
        personalPoints: const [],
        targetBand: 7,
      );
      if (draft.status == IeltsAnswerPreparationStatus.ready) {
        if (_isCurrentAnswerRequest(index, request)) {
          setState(() {
            _preparations[index] = draft;
            _answerErrors.remove(index);
          });
        }
        return;
      }
      final example = await widget.answerPreparationClient!.generate(
        id: draft.id,
        expectedVersion: draft.version,
      );
      if (_isCurrentAnswerRequest(index, request)) {
        setState(() {
          _preparations[index] = example;
          _answerErrors.remove(index);
        });
      }
    } on IeltsAnswerPreparationException catch (error) {
      if (_isCurrentAnswerRequest(index, request)) {
        setState(() => _answerErrors[index] = _answerFailureMessage(error));
      }
    } finally {
      _finishAnswerRequest(index, request);
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
                      if (_canPrepareCueCardAnswer) ...[
                        const SizedBox(height: 8),
                        Align(
                          alignment: Alignment.centerRight,
                          child: TextButton.icon(
                            key: const Key('ielts-cue-card-answer-toggle'),
                            onPressed: () {
                              setState(() {
                                _cueCardAnswerExpanded =
                                    !_cueCardAnswerExpanded;
                              });
                              if (_cueCardAnswerExpanded) {
                                unawaited(
                                  _ensureExample(
                                    0,
                                    widget.cueCardQuestionReference!,
                                  ),
                                );
                              }
                            },
                            icon: Icon(
                              _cueCardAnswerExpanded
                                  ? Icons.keyboard_arrow_up_rounded
                                  : Icons.keyboard_arrow_down_rounded,
                            ),
                            label: Text(
                              _cueCardAnswerExpanded ? '收起答案' : '示例与自定义答案',
                            ),
                          ),
                        ),
                        if (_cueCardAnswerExpanded)
                          _QuestionAnswerPanel(
                            index: 0,
                            tipText: _answerTip,
                            preparation: _preparations[0],
                            generating: _generatingQuestionIndexes.contains(0),
                            errorMessage: _answerErrors[0],
                            speaking: _speakingAnswerIndex == 0,
                            onPrepare: () => _prepareAnswer(
                              0,
                              widget.cueCardQuestionReference!,
                            ),
                            onRetry: () => _ensureExample(
                              0,
                              widget.cueCardQuestionReference!,
                            ),
                            onSpeak: _preparations[0]?.speechText == null
                                ? null
                                : () => _toggleAnswerSpeech(
                                    0,
                                    _preparations[0]!.speechText!,
                                  ),
                          ),
                      ],
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
                          onSpeak: () => _toggleQuestionSpeech(index),
                          expanded: _expandedQuestionIndex == index,
                          onToggle: _canPrepareQuestionAnswers
                              ? () {
                                  final opening =
                                      _expandedQuestionIndex != index;
                                  setState(() {
                                    _expandedQuestionIndex = opening
                                        ? index
                                        : null;
                                    _answerErrors.remove(index);
                                  });
                                  if (opening) {
                                    unawaited(
                                      _ensureExample(
                                        index,
                                        widget.questionReferences[index],
                                      ),
                                    );
                                  }
                                }
                              : null,
                        ),
                        if (_canPrepareQuestionAnswers &&
                            _expandedQuestionIndex == index)
                          _QuestionAnswerPanel(
                            index: index,
                            tipText: _answerTip,
                            preparation: _preparations[index],
                            generating: _generatingQuestionIndexes.contains(
                              index,
                            ),
                            errorMessage: _answerErrors[index],
                            speaking: _speakingAnswerIndex == index,
                            onPrepare: () => _prepareAnswer(
                              index,
                              widget.questionReferences[index],
                            ),
                            onRetry: () => _ensureExample(
                              index,
                              widget.questionReferences[index],
                            ),
                            onSpeak: _preparations[index]?.speechText == null
                                ? null
                                : () => _toggleAnswerSpeech(
                                    index,
                                    _preparations[index]!.speechText!,
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
                  onPressed: widget.onStart,
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

  String get _answerTip => switch (widget.mode) {
    PracticeMode.part1 => 'Tip：先直接回答，再补一个原因或小例子。',
    PracticeMode.part2 => 'Tip：围绕 Cue Card 要点组织开头、主体和结尾。',
    PracticeMode.part3 => 'Tip：先表明观点，再解释并补充例子或比较。',
    _ => throw StateError('IELTS answer preparation requires a section mode.'),
  };
}

bool _samePersonalPoints(List<String> left, List<String> right) {
  if (left.length != right.length) {
    return false;
  }
  for (var index = 0; index < left.length; index++) {
    if (left[index] != right[index]) {
      return false;
    }
  }
  return true;
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
    required this.expanded,
    required this.onToggle,
  });

  final int index;
  final String question;
  final bool speaking;
  final VoidCallback onSpeak;
  final bool expanded;
  final VoidCallback? onToggle;

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
        if (onToggle != null) ...[
          const SizedBox(width: 2),
          IconButton(
            key: Key('ielts-set-detail-expand-$index'),
            tooltip: expanded ? '收起第 $index 题' : '展开第 $index 题',
            onPressed: onToggle,
            icon: Icon(
              expanded
                  ? Icons.keyboard_arrow_up_rounded
                  : Icons.keyboard_arrow_down_rounded,
            ),
          ),
        ],
      ],
    ),
  );
}

class _QuestionAnswerPanel extends StatelessWidget {
  const _QuestionAnswerPanel({
    required this.index,
    required this.tipText,
    required this.preparation,
    required this.generating,
    required this.errorMessage,
    required this.speaking,
    required this.onPrepare,
    required this.onRetry,
    required this.onSpeak,
  });

  final int index;
  final String tipText;
  final IeltsAnswerPreparation? preparation;
  final bool generating;
  final String? errorMessage;
  final bool speaking;
  final VoidCallback onPrepare;
  final VoidCallback onRetry;
  final VoidCallback? onSpeak;

  @override
  Widget build(BuildContext context) {
    final ready = preparation?.status == IeltsAnswerPreparationStatus.ready;
    final personalized = ready && preparation!.personalPoints.isNotEmpty;
    return Container(
      key: Key('ielts-answer-panel-${index + 1}'),
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(0, 16, 0, 20),
      decoration: const BoxDecoration(
        border: Border(bottom: BorderSide(color: PreparationDesign.border)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(tipText, style: PreparationDesign.label),
          const SizedBox(height: 12),
          if (generating)
            const Padding(
              padding: EdgeInsets.symmetric(vertical: 20),
              child: Row(
                children: [
                  SizedBox.square(
                    dimension: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  SizedBox(width: 10),
                  Text('正在准备表达…', style: PreparationDesign.body),
                ],
              ),
            )
          else if (ready) ...[
            DecoratedBox(
              decoration: BoxDecoration(
                color: PreparationDesign.surfaceMuted,
                borderRadius: BorderRadius.circular(
                  PreparationDesign.radiusCard,
                ),
              ),
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Wrap(
                      alignment: WrapAlignment.spaceBetween,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      spacing: 8,
                      runSpacing: 4,
                      children: [
                        Text(
                          personalized ? '我的表达' : '示例回答',
                          style: PreparationDesign.label.copyWith(
                            color: PreparationDesign.inkSecondary,
                          ),
                        ),
                        TextButton.icon(
                          key: Key('ielts-speak-answer-${index + 1}'),
                          onPressed: onSpeak,
                          icon: Icon(
                            speaking
                                ? Icons.stop_rounded
                                : Icons.volume_up_outlined,
                            size: 18,
                          ),
                          label: Text(speaking ? '停止' : '播放'),
                          style: _answerActionStyle,
                        ),
                        TextButton.icon(
                          key: Key('ielts-polish-answer-${index + 1}'),
                          onPressed: onPrepare,
                          icon: const Icon(
                            Icons.auto_awesome_outlined,
                            size: 18,
                          ),
                          label: Text(personalized ? '重新润色' : '润色'),
                          style: _answerActionStyle,
                        ),
                      ],
                    ),
                    const SizedBox(height: 12),
                    _ExpandableAnswerText(text: preparation!.answer!),
                  ],
                ),
              ),
            ),
          ] else
            TextButton.icon(
              onPressed: onRetry,
              icon: const Icon(Icons.refresh_rounded),
              label: const Text('重新加载示例'),
            ),
          if (errorMessage case final message?) ...[
            const SizedBox(height: 10),
            Text(
              message,
              key: const Key('ielts-answer-error'),
              style: PreparationDesign.body.copyWith(
                color: Theme.of(context).colorScheme.error,
              ),
            ),
          ],
        ],
      ),
    );
  }

  static final ButtonStyle _answerActionStyle = TextButton.styleFrom(
    minimumSize: const Size(0, 40),
    padding: const EdgeInsets.symmetric(horizontal: 8),
    foregroundColor: PreparationDesign.ink,
  );
}

class _ExpandableAnswerText extends StatefulWidget {
  const _ExpandableAnswerText({required this.text});

  final String text;

  @override
  State<_ExpandableAnswerText> createState() => _ExpandableAnswerTextState();
}

class _ExpandableAnswerTextState extends State<_ExpandableAnswerText> {
  static const _collapsedLines = 6;
  bool _expanded = false;

  @override
  void didUpdateWidget(covariant _ExpandableAnswerText oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.text != widget.text) {
      _expanded = false;
    }
  }

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final style = PreparationDesign.body;
      final painter = TextPainter(
        text: TextSpan(text: widget.text, style: style),
        textDirection: Directionality.of(context),
        textScaler: MediaQuery.textScalerOf(context),
      )..layout(maxWidth: constraints.maxWidth);
      final canExpand = painter.computeLineMetrics().length > _collapsedLines;

      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            widget.text,
            key: const Key('ielts-answer-body'),
            maxLines: _expanded ? null : _collapsedLines,
            overflow: _expanded ? TextOverflow.visible : TextOverflow.ellipsis,
            style: style,
          ),
          if (canExpand) ...[
            const SizedBox(height: 8),
            TextButton(
              key: const Key('ielts-answer-expand-body'),
              onPressed: () => setState(() => _expanded = !_expanded),
              style: TextButton.styleFrom(
                minimumSize: const Size(0, 40),
                padding: EdgeInsets.zero,
                foregroundColor: PreparationDesign.ink,
              ),
              child: Text(_expanded ? '收起答案' : '查看完整答案'),
            ),
          ],
        ],
      );
    },
  );
}

class _PolishExperienceSheet extends StatefulWidget {
  const _PolishExperienceSheet();

  @override
  State<_PolishExperienceSheet> createState() => _PolishExperienceSheetState();
}

class _PolishExperienceSheetState extends State<_PolishExperienceSheet> {
  final _controller = TextEditingController();
  bool _showValidation = false;

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
      setState(() => _showValidation = true);
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
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text('润色成我的表达', style: PreparationDesign.sectionTitle),
        const SizedBox(height: 8),
        Text(
          '写下真实经历或想法，每行一个要点。',
          style: PreparationDesign.body.copyWith(
            color: PreparationDesign.inkSecondary,
          ),
        ),
        const SizedBox(height: 14),
        TextField(
          key: const Key('ielts-polish-experience'),
          controller: _controller,
          autofocus: true,
          minLines: 3,
          maxLines: 6,
          maxLength: 1500,
          decoration: InputDecoration(
            hintText: '例如：我通勤时喜欢听轻快的音乐，它让我更有精神。',
            errorText: _showValidation ? '请先写一条真实经历或想法。' : null,
            border: const OutlineInputBorder(),
          ),
        ),
        const SizedBox(height: 12),
        FilledButton(
          key: const Key('ielts-polish-generate'),
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

String _answerFailureMessage(IeltsAnswerPreparationException error) =>
    switch (error.kind) {
      IeltsAnswerPreparationFailureKind.authenticationRequired => '登录后才能定制答案。',
      IeltsAnswerPreparationFailureKind.generationFailed => '这次生成没有成功，请重试一次。',
      IeltsAnswerPreparationFailureKind.conflict => '答案已在其他页面更新，请重新进入后再试。',
      IeltsAnswerPreparationFailureKind.network => '网络连接失败，请检查网络后重试。',
      _ => '暂时无法生成答案，请稍后重试。',
    };
