import 'dart:math' as math;
import 'dart:typed_data';

import 'avatar_models.dart';

final class AvatarAudioException implements Exception {
  const AvatarAudioException();

  @override
  String toString() => 'Avatar audio is not supported PCM WAV.';
}

final class AvatarPcmAudio {
  const AvatarPcmAudio({required this.bytes, required this.format});

  final Uint8List bytes;
  final AvatarAudioFormat format;
}

/// Strictly extracts one PCM16, 24 kHz, mono `data` chunk from RIFF/WAVE.
///
/// Unknown RIFF chunks are skipped, including their required padding byte.
/// Truncated, trailing, duplicate, compressed, stereo, or mismatched audio is
/// rejected before any bytes reach a native SDK.
AvatarPcmAudio parseAvatarPcmWave(
  Uint8List wavBytes, {
  AvatarAudioFormat expectedFormat = AvatarAudioFormat.pcmS16le24kMono,
}) {
  if (!expectedFormat.isSupported || wavBytes.length < 44) {
    throw const AvatarAudioException();
  }
  final data = ByteData.sublistView(wavBytes);
  if (!_matchesAscii(wavBytes, 0, 'RIFF') ||
      !_matchesAscii(wavBytes, 8, 'WAVE')) {
    throw const AvatarAudioException();
  }

  final riffPayloadSize = data.getUint32(4, Endian.little);
  if (riffPayloadSize != wavBytes.length - 8) {
    throw const AvatarAudioException();
  }

  _WaveFormat? waveFormat;
  Uint8List? pcm;
  var offset = 12;
  while (offset < wavBytes.length) {
    if (wavBytes.length - offset < 8) {
      throw const AvatarAudioException();
    }
    final chunkId = String.fromCharCodes(wavBytes.sublist(offset, offset + 4));
    final chunkSize = data.getUint32(offset + 4, Endian.little);
    final payloadStart = offset + 8;
    final payloadEnd = payloadStart + chunkSize;
    if (payloadEnd < payloadStart || payloadEnd > wavBytes.length) {
      throw const AvatarAudioException();
    }

    if (chunkId == 'fmt ') {
      if (waveFormat != null || chunkSize < 16) {
        throw const AvatarAudioException();
      }
      waveFormat = _WaveFormat(
        encoding: data.getUint16(payloadStart, Endian.little),
        channels: data.getUint16(payloadStart + 2, Endian.little),
        sampleRate: data.getUint32(payloadStart + 4, Endian.little),
        byteRate: data.getUint32(payloadStart + 8, Endian.little),
        blockAlign: data.getUint16(payloadStart + 12, Endian.little),
        bitsPerSample: data.getUint16(payloadStart + 14, Endian.little),
      );
    } else if (chunkId == 'data') {
      if (pcm != null || chunkSize == 0 || chunkSize.isOdd) {
        throw const AvatarAudioException();
      }
      pcm = Uint8List.fromList(wavBytes.sublist(payloadStart, payloadEnd));
    }

    final paddedEnd = payloadEnd + (chunkSize.isOdd ? 1 : 0);
    if (paddedEnd > wavBytes.length) {
      throw const AvatarAudioException();
    }
    offset = paddedEnd;
  }

  final format = waveFormat;
  if (offset != wavBytes.length ||
      format == null ||
      pcm == null ||
      format.encoding != 1 ||
      format.channels != expectedFormat.channels ||
      format.sampleRate != expectedFormat.sampleRateHz ||
      format.bitsPerSample != 16 ||
      format.blockAlign != expectedFormat.channels * 2 ||
      format.byteRate != expectedFormat.bytesPerSecond ||
      pcm.length % format.blockAlign != 0) {
    throw const AvatarAudioException();
  }

  return AvatarPcmAudio(bytes: pcm, format: expectedFormat);
}

Iterable<Uint8List> chunkAvatarPcm(
  AvatarPcmAudio audio, {
  Duration chunkDuration = const Duration(seconds: 1),
}) sync* {
  if (audio.bytes.isEmpty ||
      !audio.format.isSupported ||
      chunkDuration <= Duration.zero) {
    throw const AvatarAudioException();
  }
  final rawChunkSize =
      (audio.format.bytesPerSecond * chunkDuration.inMicroseconds) ~/
      Duration.microsecondsPerSecond;
  final alignedChunkSize = rawChunkSize - (rawChunkSize % 2);
  if (alignedChunkSize <= 0) {
    throw const AvatarAudioException();
  }
  for (var offset = 0; offset < audio.bytes.length;) {
    final end = math.min(offset + alignedChunkSize, audio.bytes.length);
    yield Uint8List.sublistView(audio.bytes, offset, end);
    offset = end;
  }
}

bool _matchesAscii(Uint8List bytes, int offset, String value) {
  if (offset < 0 || offset + value.length > bytes.length) {
    return false;
  }
  for (var index = 0; index < value.length; index += 1) {
    if (bytes[offset + index] != value.codeUnitAt(index)) {
      return false;
    }
  }
  return true;
}

final class _WaveFormat {
  const _WaveFormat({
    required this.encoding,
    required this.channels,
    required this.sampleRate,
    required this.byteRate,
    required this.blockAlign,
    required this.bitsPerSample,
  });

  final int encoding;
  final int channels;
  final int sampleRate;
  final int byteRate;
  final int blockAlign;
  final int bitsPerSample;
}
