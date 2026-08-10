import 'dart:async';
import 'dart:typed_data';

enum Pcm16StreamCaptureFailureKind {
  emptyAudio,
  invalidAudio,
  unavailable,
  cancelled,
}

final class Pcm16StreamCaptureException implements Exception {
  const Pcm16StreamCaptureException(this.kind);

  final Pcm16StreamCaptureFailureKind kind;
}

/// Captures one native PCM16 stream while forwarding the same chunks to ASR.
///
/// The buffered bytes are converted to the local WAV evidence used by both the
/// Agent composer and Practice. Domain-specific recorders keep ownership of
/// permissions, file paths, limits, and user-facing errors.
final class Pcm16StreamCapture {
  Pcm16StreamCapture({
    required Stream<Uint8List> input,
    required this.maximumPcmBytes,
    this.sampleRate = 16000,
  }) {
    if (maximumPcmBytes < 2 || sampleRate < 1) {
      throw ArgumentError('PCM16 stream capture configuration is invalid.');
    }
    _output = StreamController<Uint8List>(onCancel: _handleOutputCancel);
    _subscription = input.listen(
      _handleChunk,
      onError: _handleInputError,
      onDone: _handleInputDone,
      cancelOnError: true,
    );
  }

  final int maximumPcmBytes;
  final int sampleRate;
  final BytesBuilder _pcm = BytesBuilder(copy: false);
  final Completer<void> _done = Completer<void>();
  late final StreamController<Uint8List> _output;
  StreamSubscription<Uint8List>? _subscription;
  Pcm16StreamCaptureException? _failure;
  bool _outputClosed = false;

  Stream<Uint8List> get stream => _output.stream;

  Future<Uint8List> finish({
    Duration timeout = const Duration(seconds: 2),
  }) async {
    final pcm = await _takeValidatedPcm(timeout);
    try {
      return _pcm16MonoWav(pcm, sampleRate: sampleRate);
    } finally {
      pcm.fillRange(0, pcm.lengthInBytes, 0);
    }
  }

  /// Finishes a streamed transcription without materializing local evidence.
  Future<void> finishAndDiscard({
    Duration timeout = const Duration(seconds: 2),
  }) async {
    Uint8List? pcm;
    try {
      pcm = await _takeValidatedPcm(timeout);
    } finally {
      _zero(pcm);
      _discardBufferedPcm();
    }
  }

  Future<Uint8List> _takeValidatedPcm(Duration timeout) async {
    try {
      await _done.future.timeout(timeout);
    } on TimeoutException {
      throw const Pcm16StreamCaptureException(
        Pcm16StreamCaptureFailureKind.unavailable,
      );
    }
    if (_failure case final failure?) {
      throw failure;
    }
    final pcm = _pcm.takeBytes();
    if (pcm.isEmpty) {
      throw const Pcm16StreamCaptureException(
        Pcm16StreamCaptureFailureKind.emptyAudio,
      );
    }
    if (pcm.lengthInBytes.isOdd || pcm.lengthInBytes > maximumPcmBytes) {
      pcm.fillRange(0, pcm.lengthInBytes, 0);
      throw const Pcm16StreamCaptureException(
        Pcm16StreamCaptureFailureKind.invalidAudio,
      );
    }
    return pcm;
  }

  Future<void> cancel() async {
    if (!_done.isCompleted) {
      _failure ??= const Pcm16StreamCaptureException(
        Pcm16StreamCaptureFailureKind.cancelled,
      );
      if (!_outputClosed) {
        _output.addError(_failure!);
      }
      await _subscription?.cancel();
      _complete();
    }
    _discardBufferedPcm();
  }

  void _handleChunk(Uint8List chunk) {
    if (_done.isCompleted) {
      return;
    }
    if (chunk.isEmpty || _pcm.length + chunk.lengthInBytes > maximumPcmBytes) {
      _fail(
        const Pcm16StreamCaptureException(
          Pcm16StreamCaptureFailureKind.invalidAudio,
        ),
      );
      return;
    }
    _pcm.add(chunk);
    _output.add(chunk);
  }

  void _handleInputError(Object _, StackTrace _) {
    _fail(
      const Pcm16StreamCaptureException(
        Pcm16StreamCaptureFailureKind.unavailable,
      ),
    );
  }

  void _handleInputDone() => _complete();

  Future<void> _handleOutputCancel() async {
    if (_done.isCompleted) {
      return;
    }
    _failure ??= const Pcm16StreamCaptureException(
      Pcm16StreamCaptureFailureKind.cancelled,
    );
    await _subscription?.cancel();
    _complete();
    _discardBufferedPcm();
  }

  void _fail(Pcm16StreamCaptureException failure) {
    if (_done.isCompleted) {
      return;
    }
    _failure = failure;
    _output.addError(failure);
    _discardBufferedPcm();
    final subscription = _subscription;
    if (subscription == null) {
      scheduleMicrotask(_complete);
      return;
    }
    unawaited(subscription.cancel().whenComplete(_complete));
  }

  void _complete() {
    if (!_done.isCompleted) {
      _done.complete();
    }
    if (!_outputClosed) {
      _outputClosed = true;
      unawaited(_output.close());
    }
  }

  void _discardBufferedPcm() => _zero(_pcm.takeBytes());

  static void _zero(Uint8List? bytes) {
    bytes?.fillRange(0, bytes.lengthInBytes, 0);
  }
}

Uint8List _pcm16MonoWav(Uint8List pcm, {required int sampleRate}) {
  final result = Uint8List(44 + pcm.lengthInBytes);
  final data = ByteData.sublistView(result);
  result.setRange(0, 4, 'RIFF'.codeUnits);
  data.setUint32(4, result.lengthInBytes - 8, Endian.little);
  result.setRange(8, 12, 'WAVE'.codeUnits);
  result.setRange(12, 16, 'fmt '.codeUnits);
  data.setUint32(16, 16, Endian.little);
  data.setUint16(20, 1, Endian.little);
  data.setUint16(22, 1, Endian.little);
  data.setUint32(24, sampleRate, Endian.little);
  data.setUint32(28, sampleRate * 2, Endian.little);
  data.setUint16(32, 2, Endian.little);
  data.setUint16(34, 16, Endian.little);
  result.setRange(36, 40, 'data'.codeUnits);
  data.setUint32(40, pcm.lengthInBytes, Endian.little);
  result.setRange(44, result.lengthInBytes, pcm);
  return result;
}
