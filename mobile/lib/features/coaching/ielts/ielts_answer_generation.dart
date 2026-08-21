import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:speakup/features/coaching/ielts/ielts_speech_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/identity/network/transport_security.dart';

final class IeltsGeneratedAnswer {
  const IeltsGeneratedAnswer({
    required this.answer,
    required this.outline,
    required this.usefulExpressions,
    required this.speechText,
  });

  final String answer;
  final List<String> outline;
  final List<String> usefulExpressions;
  final String speechText;
}

abstract interface class IeltsAnswerGenerator {
  Future<IeltsGeneratedAnswer> generate({
    required IeltsQuestionReference question,
    required List<String> personalPoints,
    double targetBand = 7,
  });
}

final class IeltsAnswerGenerationException implements Exception {
  const IeltsAnswerGenerationException({this.retryable = false});

  final bool retryable;
}

final class WireIeltsAnswerGenerator implements IeltsAnswerGenerator {
  factory WireIeltsAnswerGenerator({
    required Uri baseUri,
    required AuthSessionCredentialProvider credentialProvider,
    required AuthSessionInvalidator invalidateSession,
    IdentityHttpTransport? transport,
  }) => WireIeltsAnswerGenerator._(
    baseUri,
    SessionAuthenticatedHttpTransport(
      transport: transport ?? IoIdentityHttpTransport(),
      credentialProvider: credentialProvider,
      invalidateSession: invalidateSession,
      trustedBaseUri: baseUri,
    ),
  );

  WireIeltsAnswerGenerator._(this._baseUri, this._transport)
    : _trustedOrigin = TrustedIdentityHttpOrigin(_baseUri);

  final Uri _baseUri;
  final IdentityHttpTransport _transport;
  final TrustedIdentityHttpOrigin _trustedOrigin;

  @override
  Future<IeltsGeneratedAnswer> generate({
    required IeltsQuestionReference question,
    required List<String> personalPoints,
    double targetBand = 7,
  }) async {
    final uri = _baseUri.resolve('/v1/ielts-speaking/answers:generate');
    _trustedOrigin.validateResourceUri(uri);
    validateNoSessionCredentialInUri(uri);
    try {
      final response = await _transport
          .send(
            method: 'POST',
            uri: uri,
            headers: const <String, String>{
              HttpHeaders.acceptHeader: 'application/json',
              HttpHeaders.contentTypeHeader: 'application/json',
            },
            body: jsonEncode(<String, Object>{
              'question': <String, Object>{
                'bank_id': question.bankId,
                'part': question.part,
                'source_id': question.sourceId,
                'question_position': question.questionPosition,
              },
              'personal_points': personalPoints,
              'target_band': targetBand,
            }),
          )
          .timeout(const Duration(seconds: 45));
      if (response.statusCode != HttpStatus.ok) {
        throw IeltsAnswerGenerationException(
          retryable: response.statusCode >= 500,
        );
      }
      return _decodeGeneratedAnswer(response.body);
    } on IeltsAnswerGenerationException {
      rethrow;
    } on TimeoutException {
      throw const IeltsAnswerGenerationException(retryable: true);
    } on IOException {
      throw const IeltsAnswerGenerationException(retryable: true);
    } on StateError {
      throw const IeltsAnswerGenerationException();
    }
  }
}

IeltsGeneratedAnswer _decodeGeneratedAnswer(String body) {
  try {
    final root = jsonDecode(body);
    if (root is! Map<String, Object?> ||
        root.length != 5 ||
        root.keys.toSet().difference(const {
          'question',
          'answer',
          'outline',
          'useful_expressions',
          'speech_text',
        }).isNotEmpty) {
      throw const FormatException();
    }
    return IeltsGeneratedAnswer(
      answer: _answerString(root['answer']),
      outline: _answerStrings(root['outline']),
      usefulExpressions: _answerStrings(root['useful_expressions']),
      speechText: _answerString(root['speech_text']),
    );
  } on Object {
    throw const IeltsAnswerGenerationException();
  }
}

String _answerString(Object? value) {
  if (value is! String || value.trim().isEmpty) {
    throw const FormatException();
  }
  return value.trim();
}

List<String> _answerStrings(Object? value) {
  if (value is! List<Object?> || value.isEmpty) {
    throw const FormatException();
  }
  return value.map(_answerString).toList(growable: false);
}
