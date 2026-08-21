import 'dart:convert';
import 'dart:io';

import 'package:path_provider/path_provider.dart';

enum IeltsMockPhase {
  part1,
  part1Complete,
  part2Intro,
  part2CueCard,
  part2Preparation,
  part2Speaking,
  part2Complete,
  part3,
  complete,
}

final class IeltsMockProgress {
  const IeltsMockProgress({
    required this.sessionId,
    required this.phase,
    required this.startedAt,
    this.preparationDeadline,
    this.speakingStartedAt,
    this.speakingDeadline,
    this.part2SpokenSeconds = 0,
    this.notes = '',
  });

  final String sessionId;
  final IeltsMockPhase phase;
  final DateTime startedAt;
  final DateTime? preparationDeadline;
  final DateTime? speakingStartedAt;
  final DateTime? speakingDeadline;
  final int part2SpokenSeconds;
  final String notes;

  IeltsMockProgress copyWith({
    IeltsMockPhase? phase,
    DateTime? preparationDeadline,
    bool clearPreparationDeadline = false,
    DateTime? speakingStartedAt,
    bool clearSpeakingStartedAt = false,
    DateTime? speakingDeadline,
    bool clearSpeakingDeadline = false,
    int? part2SpokenSeconds,
    String? notes,
  }) {
    return IeltsMockProgress(
      sessionId: sessionId,
      phase: phase ?? this.phase,
      startedAt: startedAt,
      preparationDeadline: clearPreparationDeadline
          ? null
          : preparationDeadline ?? this.preparationDeadline,
      speakingStartedAt: clearSpeakingStartedAt
          ? null
          : speakingStartedAt ?? this.speakingStartedAt,
      speakingDeadline: clearSpeakingDeadline
          ? null
          : speakingDeadline ?? this.speakingDeadline,
      part2SpokenSeconds: part2SpokenSeconds ?? this.part2SpokenSeconds,
      notes: notes ?? this.notes,
    );
  }

  Map<String, Object?> toJson() => <String, Object?>{
    'session_id': sessionId,
    'phase': phase.name,
    'started_at': startedAt.toUtc().toIso8601String(),
    if (preparationDeadline != null)
      'preparation_deadline': preparationDeadline!.toUtc().toIso8601String(),
    if (speakingStartedAt != null)
      'speaking_started_at': speakingStartedAt!.toUtc().toIso8601String(),
    if (speakingDeadline != null)
      'speaking_deadline': speakingDeadline!.toUtc().toIso8601String(),
    'part_2_spoken_seconds': part2SpokenSeconds,
    'notes': notes,
  };

  static IeltsMockProgress? tryDecode(
    Object? value, {
    required String expectedSessionId,
  }) {
    if (value is! Map<String, Object?>) {
      return null;
    }
    final sessionId = value['session_id'];
    final phaseName = value['phase'];
    final startedAt = _date(value['started_at']);
    final part2SpokenSeconds = value['part_2_spoken_seconds'];
    final notes = value['notes'];
    IeltsMockPhase? phase;
    if (phaseName == 'part3Intro') {
      // Migrate checkpoints written before the redundant Part 3 ready page
      // was removed. The server session remains the source of truth.
      phase = IeltsMockPhase.part3;
    } else {
      for (final item in IeltsMockPhase.values) {
        if (item.name == phaseName) {
          phase = item;
          break;
        }
      }
    }
    if (sessionId != expectedSessionId ||
        phase == null ||
        startedAt == null ||
        part2SpokenSeconds is! int ||
        part2SpokenSeconds < 0 ||
        part2SpokenSeconds > 120 ||
        notes is! String ||
        notes.length > 4000) {
      return null;
    }
    return IeltsMockProgress(
      sessionId: expectedSessionId,
      phase: phase,
      startedAt: startedAt,
      preparationDeadline: _date(value['preparation_deadline']),
      speakingStartedAt: _date(value['speaking_started_at']),
      speakingDeadline: _date(value['speaking_deadline']),
      part2SpokenSeconds: part2SpokenSeconds,
      notes: notes,
    );
  }
}

abstract interface class IeltsMockProgressStore {
  Future<IeltsMockProgress?> read(String sessionId);

  Future<void> write(IeltsMockProgress progress);

  Future<void> delete(String sessionId);
}

final class FileIeltsMockProgressStore implements IeltsMockProgressStore {
  FileIeltsMockProgressStore({Future<Directory> Function()? supportDirectory})
    : _supportDirectory = supportDirectory ?? getApplicationSupportDirectory;

  static const _fileName = 'ielts-mock-progress-v1.json';
  final Future<Directory> Function() _supportDirectory;

  @override
  Future<IeltsMockProgress?> read(String sessionId) async {
    if (!_validSessionId(sessionId)) {
      return null;
    }
    final document = await _readDocument();
    final sessions = document['sessions'];
    if (sessions is! Map<String, Object?>) {
      return null;
    }
    return IeltsMockProgress.tryDecode(
      sessions[sessionId],
      expectedSessionId: sessionId,
    );
  }

  @override
  Future<void> write(IeltsMockProgress progress) async {
    if (!_validSessionId(progress.sessionId)) {
      throw ArgumentError.value(progress.sessionId, 'progress.sessionId');
    }
    final document = await _readDocument();
    final stored = document['sessions'];
    final sessions = stored is Map<String, Object?>
        ? Map<String, Object?>.from(stored)
        : <String, Object?>{};
    sessions[progress.sessionId] = progress.toJson();
    await _writeDocument(<String, Object?>{'version': 1, 'sessions': sessions});
  }

  @override
  Future<void> delete(String sessionId) async {
    if (!_validSessionId(sessionId)) {
      return;
    }
    final document = await _readDocument();
    final stored = document['sessions'];
    if (stored is! Map<String, Object?> || !stored.containsKey(sessionId)) {
      return;
    }
    final sessions = Map<String, Object?>.from(stored)..remove(sessionId);
    await _writeDocument(<String, Object?>{'version': 1, 'sessions': sessions});
  }

  Future<Map<String, Object?>> _readDocument() async {
    final file = await _file();
    if (!await file.exists()) {
      return <String, Object?>{'version': 1, 'sessions': <String, Object?>{}};
    }
    try {
      final value = jsonDecode(await file.readAsString());
      if (value is Map<String, Object?> && value['version'] == 1) {
        return value;
      }
    } on Object {
      // A corrupt local presentation checkpoint is ignored. Server Turns
      // remain authoritative and rebuild the safe section boundary.
    }
    return <String, Object?>{'version': 1, 'sessions': <String, Object?>{}};
  }

  Future<void> _writeDocument(Map<String, Object?> value) async {
    final file = await _file();
    await file.parent.create(recursive: true);
    final temporary = File('${file.path}.tmp');
    await temporary.writeAsString(jsonEncode(value), flush: true);
    await temporary.rename(file.path);
  }

  Future<File> _file() async {
    final root = await _supportDirectory();
    return File('${root.path}/$_fileName');
  }
}

DateTime? _date(Object? value) {
  if (value is! String) {
    return null;
  }
  return DateTime.tryParse(value)?.toUtc();
}

bool _validSessionId(String value) =>
    value.isNotEmpty &&
    value.length <= 128 &&
    value.trim() == value &&
    !value.contains(RegExp(r'[\u0000-\u001f\u007f]'));
