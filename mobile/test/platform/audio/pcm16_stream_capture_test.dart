import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/platform/audio/pcm16_stream_capture.dart';

void main() {
  test(
    'output cancellation absorbs a concurrent native cancel failure',
    () async {
      final capture = Pcm16StreamCapture(
        input: _CancelFailureStream(),
        maximumPcmBytes: 32000,
      );
      final outputErrors = <Object>[];
      final outputSubscription = capture.stream.listen(
        (_) {},
        onError: outputErrors.add,
      );

      await expectLater(outputSubscription.cancel(), completes);

      await expectLater(
        capture.finishAndDiscard(),
        throwsA(
          isA<Pcm16StreamCaptureException>().having(
            (error) => error.kind,
            'kind',
            Pcm16StreamCaptureFailureKind.cancelled,
          ),
        ),
      );
      expect(outputErrors, isEmpty);
    },
  );
}

final class _CancelFailureStream extends Stream<Uint8List> {
  @override
  StreamSubscription<Uint8List> listen(
    void Function(Uint8List event)? onData, {
    Function? onError,
    void Function()? onDone,
    bool? cancelOnError,
  }) => _CancelFailureSubscription(onError);
}

final class _CancelFailureSubscription
    implements StreamSubscription<Uint8List> {
  _CancelFailureSubscription(this._onError);

  Function? _onError;
  bool _cancelled = false;

  @override
  Future<void> cancel() {
    if (_cancelled) {
      return Future<void>.value();
    }
    _cancelled = true;
    final error = StateError('native cancel failed');
    final stackTrace = StackTrace.current;
    final onError = _onError;
    if (onError is void Function(Object, StackTrace)) {
      onError(error, stackTrace);
    } else if (onError is void Function(Object)) {
      onError(error);
    }
    return Future<void>.error(error, stackTrace);
  }

  @override
  bool get isPaused => false;

  @override
  void onData(void Function(Uint8List data)? handleData) {}

  @override
  void onDone(void Function()? handleDone) {}

  @override
  void onError(Function? handleError) => _onError = handleError;

  @override
  void pause([Future<void>? resumeSignal]) {}

  @override
  void resume() {}

  @override
  Future<E> asFuture<E>([E? futureValue]) => Future<E>.value(futureValue);
}
