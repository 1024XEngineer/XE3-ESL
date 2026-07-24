String normalizeIdentityEmailInput(String value) {
  var start = 0;
  var end = value.length;
  while (start < end && _isAsciiWhitespace(value.codeUnitAt(start))) {
    start++;
  }
  while (end > start && _isAsciiWhitespace(value.codeUnitAt(end - 1))) {
    end--;
  }
  return value.substring(start, end);
}

bool isValidIdentityEmailInput(String value) {
  final email = normalizeIdentityEmailInput(value);
  if (email.length < 3 || email.length > 254) {
    return false;
  }
  for (final codeUnit in email.codeUnits) {
    if (codeUnit < 0x21 || codeUnit > 0x7e) {
      return false;
    }
  }
  final separator = email.indexOf('@');
  if (separator <= 0 ||
      separator != email.lastIndexOf('@') ||
      separator == email.length - 1) {
    return false;
  }

  final localParts = email.substring(0, separator).split('.');
  if (localParts.any(
    (part) => part.isEmpty || part.codeUnits.any((unit) => !_isLocalUnit(unit)),
  )) {
    return false;
  }

  final domainParts = email.substring(separator + 1).split('.');
  if (domainParts.length < 2) {
    return false;
  }
  return domainParts.every((part) {
    if (part.isEmpty || part.length > 63) {
      return false;
    }
    if (!_isAlphaNumeric(part.codeUnitAt(0)) ||
        !_isAlphaNumeric(part.codeUnitAt(part.length - 1))) {
      return false;
    }
    return part.codeUnits.every(
      (unit) => _isAlphaNumeric(unit) || unit == 0x2d,
    );
  });
}

bool _isAsciiWhitespace(int codeUnit) {
  return codeUnit == 0x20 || (codeUnit >= 0x09 && codeUnit <= 0x0d);
}

bool _isLocalUnit(int codeUnit) {
  return _isAlphaNumeric(codeUnit) ||
      const <int>{
        0x21,
        0x23,
        0x24,
        0x25,
        0x26,
        0x27,
        0x2a,
        0x2b,
        0x2d,
        0x2f,
        0x3d,
        0x3f,
        0x5e,
        0x5f,
        0x60,
        0x7b,
        0x7c,
        0x7d,
        0x7e,
      }.contains(codeUnit);
}

bool _isAlphaNumeric(int codeUnit) {
  return (codeUnit >= 0x30 && codeUnit <= 0x39) ||
      (codeUnit >= 0x41 && codeUnit <= 0x5a) ||
      (codeUnit >= 0x61 && codeUnit <= 0x7a);
}
