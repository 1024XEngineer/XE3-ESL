import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/ielts/ielts_mock_progress_store.dart';

void main() {
  late Directory directory;
  late FileIeltsMockProgressStore store;

  setUp(() async {
    directory = await Directory.systemTemp.createTemp(
      'speakup-ielts-progress-',
    );
    store = FileIeltsMockProgressStore(supportDirectory: () async => directory);
  });

  tearDown(() async {
    if (await directory.exists()) {
      await directory.delete(recursive: true);
    }
  });

  test('round trips notes, timers, section, and Part 2 duration', () async {
    final startedAt = DateTime.utc(2026, 7, 29, 8);
    final progress = IeltsMockProgress(
      sessionId: 'session-ielts-1',
      phase: IeltsMockPhase.part2Speaking,
      startedAt: startedAt,
      preparationDeadline: startedAt.add(const Duration(minutes: 1)),
      speakingStartedAt: startedAt.add(const Duration(minutes: 1)),
      speakingDeadline: startedAt.add(const Duration(minutes: 3)),
      part2SpokenSeconds: 87,
      notes: 'skill · reason · learning plan',
    );

    await store.write(progress);
    final restored = await store.read(progress.sessionId);

    expect(restored, isNotNull);
    expect(restored!.sessionId, progress.sessionId);
    expect(restored.phase, IeltsMockPhase.part2Speaking);
    expect(restored.startedAt, startedAt);
    expect(restored.preparationDeadline, progress.preparationDeadline);
    expect(restored.speakingStartedAt, progress.speakingStartedAt);
    expect(restored.speakingDeadline, progress.speakingDeadline);
    expect(restored.part2SpokenSeconds, 87);
    expect(restored.notes, progress.notes);
  });

  test('isolates sessions and deletes only the selected checkpoint', () async {
    final startedAt = DateTime.utc(2026, 7, 29, 8);
    for (final id in const ['session-a', 'session-b']) {
      await store.write(
        IeltsMockProgress(
          sessionId: id,
          phase: IeltsMockPhase.part1,
          startedAt: startedAt,
        ),
      );
    }

    await store.delete('session-a');

    expect(await store.read('session-a'), isNull);
    expect(await store.read('session-b'), isNotNull);
  });

  test('ignores a corrupt local checkpoint', () async {
    await File(
      '${directory.path}/ielts-mock-progress-v1.json',
    ).writeAsString('{not-json');

    expect(await store.read('session-a'), isNull);
  });

  test('migrates the removed Part 3 intro checkpoint into practice', () async {
    const sessionId = 'session-part-3';
    await File('${directory.path}/ielts-mock-progress-v1.json').writeAsString(
      jsonEncode(<String, Object?>{
        'version': 1,
        'sessions': <String, Object?>{
          sessionId: <String, Object?>{
            'session_id': sessionId,
            'phase': 'part3Intro',
            'started_at': '2026-08-21T07:00:00.000Z',
            'part_2_spoken_seconds': 0,
            'notes': '',
          },
        },
      }),
    );

    final restored = await store.read(sessionId);

    expect(restored, isNotNull);
    expect(restored!.phase, IeltsMockPhase.part3);
  });
}
