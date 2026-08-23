import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/config/app_environment.dart';

void main() {
  group('resolveApiBaseUri', () {
    test('injects the staging API for staging release', () {
      expect(
        resolveApiBaseUri(
          flavor: 'staging',
          isReleaseMode: true,
          isAndroid: true,
          explicitBaseUrl: '',
        ),
        Uri.parse('https://staging-api.speak-up.top'),
      );
    });

    test('accepts the contracted production API injection', () {
      expect(
        resolveApiBaseUri(
          flavor: 'production',
          isReleaseMode: true,
          isAndroid: true,
          explicitBaseUrl: 'https://api.speak-up.top',
        ),
        Uri.parse('https://api.speak-up.top'),
      );
    });

    test('rejects an API override that disagrees with the release flavor', () {
      expect(
        () => resolveApiBaseUri(
          flavor: 'production',
          isReleaseMode: true,
          isAndroid: true,
          explicitBaseUrl: 'http://127.0.0.1:8080',
        ),
        throwsStateError,
      );
    });

    test('rejects unflavored Android release even with an explicit API', () {
      expect(
        () => resolveApiBaseUri(
          flavor: null,
          isReleaseMode: true,
          isAndroid: true,
          explicitBaseUrl: 'https://api.speak-up.top',
        ),
        throwsStateError,
      );
    });

    test('keeps explicit local API injection for development', () {
      expect(
        resolveApiBaseUri(
          flavor: 'staging',
          isReleaseMode: false,
          isAndroid: true,
          explicitBaseUrl: 'http://127.0.0.1:18080',
        ),
        Uri.parse('http://127.0.0.1:18080'),
      );
    });

    test('allows an explicit production API for unflavored iOS release', () {
      expect(
        resolveApiBaseUri(
          flavor: null,
          isReleaseMode: true,
          isAndroid: false,
          explicitBaseUrl: 'https://api.speak-up.top',
        ),
        Uri.parse('https://api.speak-up.top'),
      );
    });

    test('rejects localhost for unflavored iOS release', () {
      expect(
        () => resolveApiBaseUri(
          flavor: null,
          isReleaseMode: true,
          isAndroid: false,
          explicitBaseUrl: 'https://localhost:8080',
        ),
        throwsStateError,
      );
    });
  });
}
