import 'package:flutter_tts/flutter_tts.dart';

abstract interface class PracticePromptSpeaker {
  Future<void> speak(String text);

  Future<void> stop();

  Future<void> dispose();
}

abstract interface class CoachingSpeechPlayer implements PracticePromptSpeaker {
  Future<void> speakQuestion({
    required String questionId,
    required String fallbackText,
  });
}

final class ModelSpeechPlaybackException implements Exception {
  const ModelSpeechPlaybackException({required this.audioStarted});

  final bool audioStarted;
}

typedef ModelTextSpeech = Future<void> Function(String text);
typedef ModelQuestionSpeech =
    Future<void> Function(String questionId, String fallbackText);
typedef ModelSpeechStop = Future<void> Function();

final class ModelFirstPracticePromptSpeaker implements CoachingSpeechPlayer {
  ModelFirstPracticePromptSpeaker({
    required this._speakTextWithModel,
    required this._speakQuestionWithModel,
    required this._stopModelSpeech,
  });

  final ModelTextSpeech _speakTextWithModel;
  final ModelQuestionSpeech _speakQuestionWithModel;
  final ModelSpeechStop _stopModelSpeech;
  PracticePromptSpeaker? _fallback;
  int _generation = 0;
  bool _disposed = false;

  @override
  Future<void> speak(String text) =>
      _play(text, () => _speakTextWithModel(text));

  @override
  Future<void> speakQuestion({
    required String questionId,
    required String fallbackText,
  }) => _play(
    fallbackText,
    () => _speakQuestionWithModel(questionId, fallbackText),
  );

  Future<void> _play(String fallbackText, Future<void> Function() model) async {
    if (_disposed) {
      throw StateError('Coaching speech player is disposed.');
    }
    final generation = ++_generation;
    final existingFallback = _fallback;
    if (existingFallback != null) {
      await _bestEffort(existingFallback.stop);
    }
    try {
      await model();
    } on ModelSpeechPlaybackException catch (error) {
      if (error.audioStarted || _disposed || generation != _generation) {
        rethrow;
      }
      final fallback = _fallback ??= SystemPracticePromptSpeaker();
      await fallback.speak(fallbackText);
    }
  }

  @override
  Future<void> stop() async {
    _generation++;
    final existingFallback = _fallback;
    await Future.wait<void>([
      _bestEffort(_stopModelSpeech),
      if (existingFallback != null) _bestEffort(existingFallback.stop),
    ]);
  }

  @override
  Future<void> dispose() async {
    if (_disposed) {
      return;
    }
    _disposed = true;
    _generation++;
    final existingFallback = _fallback;
    _fallback = null;
    await Future.wait<void>([
      _bestEffort(_stopModelSpeech),
      if (existingFallback != null) _bestEffort(existingFallback.dispose),
    ]);
  }

  Future<void> _bestEffort(Future<void> Function() operation) async {
    try {
      await operation();
    } catch (_) {
      // Stopping either playback implementation must never block navigation
      // or disposal of the owning screen.
    }
  }
}

final class SystemPracticePromptSpeaker implements PracticePromptSpeaker {
  SystemPracticePromptSpeaker({FlutterTts? flutterTts})
    : _flutterTts = flutterTts ?? FlutterTts();

  final FlutterTts _flutterTts;
  bool _configured = false;

  static const _preferredMaleVoiceNames = <String>{
    'aaron',
    'alex',
    'daniel',
    'eddy',
    'fred',
    'ralph',
    'reed',
    'rocko',
  };

  Future<void> _configure() async {
    if (_configured) {
      return;
    }
    await _flutterTts.setLanguage('en-US');
    await _selectEnglishMaleVoice();
    await _flutterTts.setSpeechRate(0.45);
    await _flutterTts.setPitch(0.9);
    await _flutterTts.setVolume(1.0);
    await _flutterTts.awaitSpeakCompletion(true);
    _configured = true;
  }

  Future<void> _selectEnglishMaleVoice() async {
    try {
      final rawVoices = await _flutterTts.getVoices;
      if (rawVoices is! List) {
        return;
      }
      Map<String, String>? best;
      var bestRank = 1 << 20;
      for (final rawVoice in rawVoices) {
        if (rawVoice is! Map) {
          continue;
        }
        final name = rawVoice['name']?.toString().trim() ?? '';
        final locale = rawVoice['locale']?.toString().trim() ?? '';
        final gender =
            rawVoice['gender']?.toString().trim().toLowerCase() ?? '';
        if (name.isEmpty || !locale.toLowerCase().startsWith('en')) {
          continue;
        }
        final knownMale = _preferredMaleVoiceNames.contains(name.toLowerCase());
        final declaredMale = gender == 'male';
        if (!declaredMale && !knownMale) {
          continue;
        }
        final american = locale.toLowerCase().startsWith('en-us');
        final rank =
            (declaredMale ? 0 : 10) + (american ? 0 : 2) + (knownMale ? 0 : 1);
        if (rank < bestRank) {
          bestRank = rank;
          best = <String, String>{'name': name, 'locale': locale};
        }
      }
      if (best != null) {
        await _flutterTts.setVoice(best);
      } else {
        await _flutterTts.setVoice(const <String, String>{
          'name': 'Aaron',
          'locale': 'en-US',
        });
      }
    } catch (_) {
      // Voice enumeration varies by platform. The lower-pitch en-US fallback
      // remains usable when a named male system voice is unavailable.
    }
  }

  @override
  Future<void> speak(String text) async {
    final value = text.trim();
    if (value.isEmpty) {
      throw StateError('IELTS examiner narration cannot be empty.');
    }
    await _configure();
    await _flutterTts.stop();
    final result = await _flutterTts.speak(value);
    if (result != 1) {
      throw StateError('IELTS examiner narration did not start.');
    }
  }

  @override
  Future<void> stop() async {
    await _flutterTts.stop();
  }

  @override
  Future<void> dispose() async {
    await _flutterTts.stop();
  }
}
