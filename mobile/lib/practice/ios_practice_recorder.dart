import 'dart:io';
import 'dart:math';

import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';
import 'package:speakup/practice/practice_recording.dart';

abstract interface class NativePracticeRecorder {
  Future<bool> hasPermission();

  Future<void> startWav(String path);

  Future<String?> stop();
}

final class RecordNativePracticeRecorder implements NativePracticeRecorder {
  RecordNativePracticeRecorder([AudioRecorder? recorder])
    : _recorder = recorder ?? AudioRecorder();

  final AudioRecorder _recorder;

  @override
  Future<bool> hasPermission() => _recorder.hasPermission();

  @override
  Future<void> startWav(String path) {
    return _recorder.start(
      const RecordConfig(
        encoder: AudioEncoder.wav,
        sampleRate: 16000,
        numChannels: 1,
        bitRate: 256000,
        autoGain: false,
        echoCancel: false,
        noiseSuppress: false,
      ),
      path: path,
    );
  }

  @override
  Future<String?> stop() => _recorder.stop();
}

final class IosPracticeRecorder implements PracticeRecorder {
  IosPracticeRecorder({
    NativePracticeRecorder? recorder,
    Future<Directory> Function()? temporaryDirectory,
  }) : _recorder = recorder ?? RecordNativePracticeRecorder(),
       _temporaryDirectory = temporaryDirectory ?? getTemporaryDirectory;

  static const _managedDirectoryName = 'speakup-practice-audio';
  static const _minimumWavBytes = 45;

  final NativePracticeRecorder _recorder;
  final Future<Directory> Function() _temporaryDirectory;
  final Random _random = Random.secure();
  String? _activePath;

  @override
  Future<void> start() async {
    if (_activePath != null) {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.alreadyRecording,
      );
    }
    if (!await _recorder.hasPermission()) {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.permissionDenied,
      );
    }
    final directory = await _managedDirectory();
    final path = '${directory.path}/${_newFileName()}';
    _activePath = path;
    try {
      await _recorder.startWav(path);
    } catch (_) {
      _activePath = null;
      await _deletePath(path);
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.unavailable,
      );
    }
  }

  @override
  Future<RecordedPracticeAudio> stop() async {
    final expectedPath = _activePath;
    if (expectedPath == null) {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.notRecording,
      );
    }
    _activePath = null;
    String? stoppedPath;
    try {
      stoppedPath = await _recorder.stop();
    } catch (_) {
      await _deletePath(expectedPath);
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.unavailable,
      );
    }
    final path = stoppedPath ?? expectedPath;
    if (path != expectedPath) {
      await _deletePath(expectedPath);
    }
    if (!await _isManagedPath(path)) {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.invalidAudio,
      );
    }
    final file = File(path);
    if (!await file.exists()) {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.emptyAudio,
      );
    }
    final size = await file.length();
    if (size < _minimumWavBytes) {
      await _deletePath(path);
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.emptyAudio,
      );
    }
    return RecordedPracticeAudio(
      path: path,
      contentType: 'audio/wav',
      sizeBytes: size,
    );
  }

  @override
  Future<void> discardCurrent() async {
    final path = _activePath;
    _activePath = null;
    if (path != null) {
      try {
        await _recorder.stop();
      } catch (_) {
        // Cleanup still removes the owned temporary path.
      }
      await _deletePath(path);
    }
  }

  @override
  Future<void> discard(RecordedPracticeAudio audio) async {
    if (!await _isManagedPath(audio.path)) {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.invalidAudio,
      );
    }
    await _deletePath(audio.path);
  }

  @override
  Future<void> clearAccountState() async {
    await discardCurrent();
    final directory = await _managedDirectory(create: false);
    if (!await directory.exists()) {
      return;
    }
    await for (final entity in directory.list(followLinks: false)) {
      if (entity is File && entity.path.endsWith('.wav')) {
        await _deletePath(entity.path);
      }
    }
  }

  Future<bool> _isManagedPath(String path) async {
    final directory = await _managedDirectory(create: false);
    final file = File(path);
    if (!await directory.exists() ||
        await FileSystemEntity.type(path, followLinks: false) !=
            FileSystemEntityType.file) {
      return false;
    }
    final canonicalRoot = await directory.resolveSymbolicLinks();
    final canonicalFile = await file.resolveSymbolicLinks();
    return canonicalFile.startsWith(
          '$canonicalRoot${Platform.pathSeparator}',
        ) &&
        canonicalFile.endsWith('.wav');
  }

  Future<Directory> _managedDirectory({bool create = true}) async {
    final root = await _temporaryDirectory();
    final directory = Directory('${root.path}/$_managedDirectoryName');
    if (create && !await directory.exists()) {
      await directory.create(recursive: true);
    }
    return directory;
  }

  String _newFileName() {
    final buffer = StringBuffer('turn-');
    for (var index = 0; index < 16; index++) {
      buffer.write(_random.nextInt(256).toRadixString(16).padLeft(2, '0'));
    }
    return '$buffer.wav';
  }

  Future<void> _deletePath(String path) async {
    try {
      final file = File(path);
      if (await file.exists()) {
        await file.delete();
      }
    } on FileSystemException {
      throw const PracticeRecordingException(
        PracticeRecordingFailureKind.unavailable,
      );
    }
  }
}
