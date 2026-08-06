import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/identity/auth_input.dart';

void main() {
  test('email normalization removes only surrounding ASCII whitespace', () {
    expect(
      normalizeIdentityEmailInput('\t \r\nlearner@example.com\u00a0'),
      'learner@example.com\u00a0',
    );
  });

  test('email normalization keeps internal whitespace unchanged', () {
    expect(
      normalizeIdentityEmailInput('learner @example.com'),
      'learner @example.com',
    );
  });

  test('email validation accepts ASCII and rejects Unicode addresses', () {
    expect(isValidIdentityEmailInput(' learner@example.com '), isTrue);
    expect(isValidIdentityEmailInput('learner @example.com'), isFalse);
    expect(isValidIdentityEmailInput('learner@例子.test'), isFalse);
  });

  test('email validation matches the v1 local and domain boundaries', () {
    expect(isValidIdentityEmailInput('first.last+tag@xn--fsq.example'), isTrue);
    expect(isValidIdentityEmailInput('.learner@example.com'), isFalse);
    expect(isValidIdentityEmailInput('learner..name@example.com'), isFalse);
    expect(isValidIdentityEmailInput('learner@localhost'), isFalse);
    expect(isValidIdentityEmailInput('learner@-example.com'), isFalse);
    expect(isValidIdentityEmailInput('learner@example-.com'), isFalse);
  });
}
