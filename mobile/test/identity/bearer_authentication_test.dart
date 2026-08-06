import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/identity/network/bearer_authentication.dart';

void main() {
  test('formats an opaque Session Token as a Bearer credential', () {
    expect(
      bearerAuthorizationValue('sess_ABC-123~/+='),
      'Bearer sess_ABC-123~/+=',
    );
  });

  test('rejects credentials that could inject an additional header', () {
    const token = 'sess_opaque-secret\r\nX-Leak: value';

    expect(
      () => bearerAuthorizationValue(token),
      throwsA(
        isA<ArgumentError>().having(
          (error) => error.toString(),
          'safe message',
          isNot(contains(token)),
        ),
      ),
    );
  });

  test('validates the complete RFC 6750 b64token representation', () {
    expect(isValidOpaqueSessionToken('sess_abc123=='), isTrue);
    expect(isValidOpaqueSessionToken(''), isFalse);
    expect(isValidOpaqueSessionToken('abc123=='), isFalse);
    expect(isValidOpaqueSessionToken('abc=def'), isFalse);
    expect(isValidOpaqueSessionToken('sess_abc def'), isFalse);
  });
}
