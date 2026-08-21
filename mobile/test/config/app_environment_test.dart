import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/config/app_environment.dart';

void main() {
  group('resolveApiBaseUri', () {
    test('injects the staging API for staging release', () {
      expect(
        resolveApiBaseUri(
          flavor: 'staging',
          isReleaseMode: true,
          explicitBaseUrl: '',
        ),
        Uri.parse('https://staging-api.speak-up.top'),
      );
    });

    test('injects the production API for production release', () {
      expect(
        resolveApiBaseUri(
          flavor: 'production',
          isReleaseMode: true,
          explicitBaseUrl: '',
        ),
        Uri.parse('https://api.speak-up.top'),
      );
    });

    test('rejects an API override that disagrees with the release flavor', () {
      expect(
        () => resolveApiBaseUri(
          flavor: 'production',
          isReleaseMode: true,
          explicitBaseUrl: 'http://127.0.0.1:8080',
        ),
        throwsStateError,
      );
    });

    test('rejects release builds without a known flavor', () {
      expect(
        () => resolveApiBaseUri(
          flavor: null,
          isReleaseMode: true,
          explicitBaseUrl: '',
        ),
        throwsStateError,
      );
    });

    test('keeps explicit local API injection for development', () {
      expect(
        resolveApiBaseUri(
          flavor: 'staging',
          isReleaseMode: false,
          explicitBaseUrl: 'http://127.0.0.1:18080',
        ),
        Uri.parse('http://127.0.0.1:18080'),
      );
    });
  });
}
