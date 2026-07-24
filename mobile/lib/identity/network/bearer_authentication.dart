import 'package:speakup/identity/model/identity_models.dart';

String bearerAuthorizationValue(String sessionToken) {
  if (!isValidOpaqueSessionToken(sessionToken)) {
    throw ArgumentError('Session Token is not a valid Bearer credential.');
  }
  return 'Bearer $sessionToken';
}
