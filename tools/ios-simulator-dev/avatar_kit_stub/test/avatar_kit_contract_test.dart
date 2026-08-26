import 'package:avatar_kit/avatar_kit.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('load mirrors the compressed-model call used by SpeakUp', () async {
    await expectLater(
      AvatarManager.shared.load(
        id: 'avatar-contract-test',
        useCompressedModel: true,
      ),
      throwsA(isA<UnsupportedError>()),
    );
  });
}
