import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/practice/avatar/avatar.dart';

import 'avatar_test_fakes.dart';

void main() {
  group('parseAvatarPcmWave', () {
    test('extracts strict 24 kHz mono PCM across padded unknown chunks', () {
      final pcm = Uint8List.fromList(List<int>.generate(32, (index) => index));
      final wav = buildPcmWave(
        pcm: pcm,
        beforeFormat: [
          (id: 'JUNK', bytes: Uint8List.fromList([1, 2, 3])),
        ],
        betweenFormatAndData: [
          (id: 'LIST', bytes: Uint8List.fromList([4, 5, 6, 7])),
        ],
      );

      final parsed = parseAvatarPcmWave(wav);

      expect(parsed.bytes, pcm);
      expect(parsed.format, same(AvatarAudioFormat.pcmS16le24kMono));
    });

    test('rejects stereo, wrong rate, compressed, and inconsistent fmt', () {
      for (final wav in [
        buildPcmWave(channels: 2),
        buildPcmWave(sampleRate: 16000),
        buildPcmWave(audioEncoding: 3),
        buildPcmWave(byteRate: 1234),
        buildPcmWave(blockAlign: 4),
      ]) {
        expect(
          () => parseAvatarPcmWave(wav),
          throwsA(isA<AvatarAudioException>()),
        );
      }
    });

    test('rejects truncated, trailing, duplicate, and odd PCM payloads', () {
      final valid = buildPcmWave(pcm: Uint8List(32));
      final truncated = Uint8List.fromList(valid.sublist(0, valid.length - 1));
      final trailing = Uint8List.fromList([...valid, 0]);
      final duplicateData = buildPcmWave(
        pcm: Uint8List(32),
        afterData: [(id: 'data', bytes: Uint8List(2))],
      );
      final oddPcm = buildPcmWave(pcm: Uint8List(3));

      for (final wav in [truncated, trailing, duplicateData, oddPcm]) {
        expect(
          () => parseAvatarPcmWave(wav),
          throwsA(isA<AvatarAudioException>()),
        );
      }
    });
  });

  test('chunks on PCM16 frame boundaries without dropping bytes', () {
    final pcm = Uint8List.fromList(
      List<int>.generate(100000, (index) => index % 256),
    );
    final chunks = chunkAvatarPcm(
      AvatarPcmAudio(bytes: pcm, format: AvatarAudioFormat.pcmS16le24kMono),
    ).toList();

    expect(chunks.map((chunk) => chunk.length), [48000, 48000, 4000]);
    expect(chunks.expand((chunk) => chunk), pcm);
    expect(chunks.every((chunk) => chunk.length.isEven), isTrue);
  });
}
