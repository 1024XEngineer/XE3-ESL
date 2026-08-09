final class IeltsAnswerQuestionReference {
  const IeltsAnswerQuestionReference({
    required this.bankId,
    required this.part,
    required this.sourceId,
    required this.questionPosition,
  });

  final String bankId;
  final String part;
  final String sourceId;
  final int questionPosition;

  Map<String, Object> toJson() => <String, Object>{
    'bank_id': bankId,
    'part': part,
    'source_id': sourceId,
    'question_position': questionPosition,
  };
}

enum IeltsAnswerPreparationStatus { draft, generating, ready, failed }

final class IeltsAnswerPreparation {
  const IeltsAnswerPreparation({
    required this.id,
    required this.question,
    required this.personalPoints,
    required this.targetBand,
    required this.status,
    required this.version,
    required this.generationRevision,
    required this.answer,
    required this.outline,
    required this.usefulExpressions,
    required this.speechText,
  });

  final String id;
  final IeltsAnswerQuestionReference question;
  final List<String> personalPoints;
  final double targetBand;
  final IeltsAnswerPreparationStatus status;
  final int version;
  final int generationRevision;
  final String? answer;
  final List<String> outline;
  final List<String> usefulExpressions;
  final String? speechText;
}

enum IeltsAnswerPreparationFailureKind {
  authenticationRequired,
  invalidRequest,
  notFound,
  conflict,
  generationFailed,
  network,
  invalidResponse,
  server,
}

final class IeltsAnswerPreparationException implements Exception {
  const IeltsAnswerPreparationException({
    required this.kind,
    this.statusCode,
    this.retryable = false,
  });

  final IeltsAnswerPreparationFailureKind kind;
  final int? statusCode;
  final bool retryable;
}

abstract interface class IeltsAnswerPreparationClient {
  Future<IeltsAnswerPreparation> create({
    required IeltsAnswerQuestionReference question,
    required List<String> personalPoints,
    required double targetBand,
  });

  Future<IeltsAnswerPreparation> get(String id);

  Future<IeltsAnswerPreparation> update({
    required String id,
    required int expectedVersion,
    required List<String> personalPoints,
    required double targetBand,
  });

  Future<IeltsAnswerPreparation> generate({
    required String id,
    required int expectedVersion,
  });

  Future<void> delete({required String id, required int expectedVersion});
}
