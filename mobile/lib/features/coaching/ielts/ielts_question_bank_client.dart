import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';

enum IeltsQuestionBankFailureKind { network, unavailable, invalidResponse }

final class IeltsQuestionBankClientException implements Exception {
  const IeltsQuestionBankClientException({
    required this.kind,
    this.statusCode,
    this.retryable = false,
  });

  final IeltsQuestionBankFailureKind kind;
  final int? statusCode;
  final bool retryable;

  @override
  String toString() => 'IeltsQuestionBankClientException(kind: ${kind.name})';
}

abstract interface class IeltsQuestionBankClient {
  Future<IeltsQuestionBank> getQuestionBank();
}
