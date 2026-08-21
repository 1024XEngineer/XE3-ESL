enum InterviewPreparationSource {
  jobDescription('job_description'),
  quickStart('quick_start');

  const InterviewPreparationSource(this.wireValue);

  final String wireValue;
}

final class InterviewPreparationInput {
  const InterviewPreparationInput({
    required this.source,
    this.jobTitle,
    this.jobDescription,
    this.company,
    this.seniority,
    this.candidateBackground,
    this.practiceFocus,
  });

  final InterviewPreparationSource source;
  final String? jobTitle;
  final String? jobDescription;
  final String? company;
  final String? seniority;
  final String? candidateBackground;
  final String? practiceFocus;

  @override
  bool operator ==(Object other) =>
      other is InterviewPreparationInput &&
      other.source == source &&
      other.jobTitle == jobTitle &&
      other.jobDescription == jobDescription &&
      other.company == company &&
      other.seniority == seniority &&
      other.candidateBackground == candidateBackground &&
      other.practiceFocus == practiceFocus;

  @override
  int get hashCode => Object.hash(
    source,
    jobTitle,
    jobDescription,
    company,
    seniority,
    candidateBackground,
    practiceFocus,
  );
}

final class InterviewCatalogRecommendation {
  const InterviewCatalogRecommendation({
    required this.sceneId,
    required this.sceneVersion,
    required this.selectedRoleIds,
    required this.practiceOptionId,
  });

  final String sceneId;
  final int sceneVersion;
  final List<String> selectedRoleIds;
  final String practiceOptionId;
}

final class InterviewPreparationCandidate {
  const InterviewPreparationCandidate({
    required this.source,
    required this.generalAdviceOnly,
    required this.jobTitle,
    this.company = '',
    required this.seniority,
    required this.responsibilities,
    required this.coreSkills,
    required this.communicationFocus,
    required this.practiceGoals,
    required this.scopeNotice,
    required this.catalogRecommendation,
  });

  final InterviewPreparationSource source;
  final bool generalAdviceOnly;
  final String jobTitle;
  final String company;
  final String seniority;
  final List<String> responsibilities;
  final List<String> coreSkills;
  final List<String> communicationFocus;
  final List<String> practiceGoals;
  final String scopeNotice;
  final InterviewCatalogRecommendation catalogRecommendation;
}

enum InterviewPreparationStatus { draft, confirmed, discarded }

final class InterviewPreparation {
  const InterviewPreparation({
    required this.id,
    required this.userId,
    required this.input,
    required this.candidate,
    required this.status,
    required this.version,
    required this.createdAt,
    required this.updatedAt,
    this.resumeUsed = false,
  });

  final String id;
  final String userId;
  final InterviewPreparationInput input;
  final InterviewPreparationCandidate candidate;
  final bool resumeUsed;
  final InterviewPreparationStatus status;
  final int version;
  final DateTime createdAt;
  final DateTime updatedAt;
}

final class InterviewPreparationSnapshot {
  const InterviewPreparationSnapshot({
    required this.id,
    required this.version,
    required this.input,
    required this.candidate,
    this.resumeUsed = false,
  });

  final String id;
  final int version;
  final InterviewPreparationInput input;
  final InterviewPreparationCandidate candidate;
  final bool resumeUsed;
}

enum JobPreparationOperationStage {
  interviewPreparation,
  confirmation,
  plan,
  session,
  voice,
}

enum JobPreparationFailureKind {
  authenticationRequired,
  invalidRequest,
  notFound,
  conflict,
  network,
  server,
  invalidResponse,
  superseded,
}

final class JobPreparationException implements Exception {
  const JobPreparationException({
    required this.kind,
    this.stage,
    this.statusCode,
    this.errorCode,
    this.retryable = false,
  });

  final JobPreparationFailureKind kind;
  final JobPreparationOperationStage? stage;
  final int? statusCode;
  final String? errorCode;
  final bool retryable;

  @override
  String toString() =>
      'JobPreparationException(kind: ${kind.name}, stage: ${stage?.name})';
}
