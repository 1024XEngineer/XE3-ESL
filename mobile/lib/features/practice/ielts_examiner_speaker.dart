import 'package:flutter_tts/flutter_tts.dart';

abstract interface class IeltsExaminerSpeaker {
  Future<void> speak(String text);

  Future<void> stop();

  Future<void> dispose();
}

final class SystemIeltsExaminerSpeaker implements IeltsExaminerSpeaker {
  SystemIeltsExaminerSpeaker({FlutterTts? flutterTts})
    : _flutterTts = flutterTts ?? FlutterTts();

  final FlutterTts _flutterTts;
  bool _configured = false;

  Future<void> _configure() async {
    if (_configured) {
      return;
    }
    await _flutterTts.setLanguage('en-US');
    await _flutterTts.setSpeechRate(0.45);
    await _flutterTts.setPitch(1.0);
    await _flutterTts.setVolume(1.0);
    await _flutterTts.awaitSpeakCompletion(true);
    _configured = true;
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
