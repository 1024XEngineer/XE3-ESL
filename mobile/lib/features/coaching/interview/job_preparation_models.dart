enum JobTargetSource {
  jobDescription('job_description'),
  quickStart('quick_start');

  const JobTargetSource(this.wireValue);

  final String wireValue;
}

enum JobTargetStage {
  draft('draft'),
  parsing('parsing'),
  analysisFailed('analysis_failed'),
  awaitingConfirmation('awaiting_confirmation'),
  confirmed('confirmed'),
  discarded('discarded');

  const JobTargetStage(this.wireValue);

  final String wireValue;
}

enum JobTargetAnalysisStatus {
  running('running'),
  succeeded('succeeded'),
  failed('failed');

  const JobTargetAnalysisStatus(this.wireValue);

  final String wireValue;
}

final class JobTargetInput {
  const JobTargetInput({
    required this.source,
    this.jobTitle,
    this.jobDescription,
    this.company,
    this.seniority,
    this.candidateBackground,
    this.resumeRef,
    this.practiceFocus,
  });

  final JobTargetSource source;
  final String? jobTitle;
  final String? jobDescription;
  final String? company;
  final String? seniority;
  final String? candidateBackground;
  final String? resumeRef;
  final String? practiceFocus;

  @override
  bool operator ==(Object other) {
    return other is JobTargetInput &&
        other.source == source &&
        other.jobTitle == jobTitle &&
        other.jobDescription == jobDescription &&
        other.company == company &&
        other.seniority == seniority &&
        other.candidateBackground == candidateBackground &&
        other.resumeRef == resumeRef &&
        other.practiceFocus == practiceFocus;
  }

  @override
  int get hashCode => Object.hash(
    source,
    jobTitle,
    jobDescription,
    company,
    seniority,
    candidateBackground,
    resumeRef,
    practiceFocus,
  );
}

final class JobPreparationResumeSelection {
  const JobPreparationResumeSelection({
    required this.resumeId,
    required this.revision,
    required this.resourceVersion,
    required this.temporary,
    required this.title,
  });

  final String resumeId;
  final int revision;
  final int resourceVersion;
  final bool temporary;
  final String title;
}

final class JobTargetCatalogRecommendation {
  const JobTargetCatalogRecommendation({
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

final class JobTargetCandidate {
  const JobTargetCandidate({
    required this.source,
    required this.generalAdviceOnly,
    required this.jobTitle,
    required this.seniority,
    required this.responsibilities,
    required this.coreSkills,
    required this.communicationFocus,
    required this.practiceGoals,
    required this.scopeNotice,
    required this.catalogRecommendation,
  });

  final JobTargetSource source;
  final bool generalAdviceOnly;
  final String jobTitle;
  final String seniority;
  final List<String> responsibilities;
  final List<String> coreSkills;
  final List<String> communicationFocus;
  final List<String> practiceGoals;
  final String scopeNotice;
  final JobTargetCatalogRecommendation catalogRecommendation;
}

final class JobTargetAnalysis {
  const JobTargetAnalysis({
    required this.inputVersion,
    required this.analysisVersion,
    required this.attempt,
    required this.status,
    required this.startedAt,
    this.candidate,
    this.stableErrorCategory,
    this.finishedAt,
  });

  final int inputVersion;
  final int analysisVersion;
  final int attempt;
  final JobTargetAnalysisStatus status;
  final JobTargetCandidate? candidate;
  final String? stableErrorCategory;
  final DateTime startedAt;
  final DateTime? finishedAt;
}

final class JobTargetConfirmation {
  const JobTargetConfirmation({
    required this.inputVersion,
    required this.analysisVersion,
    required this.confirmationVersion,
    required this.candidate,
    required this.confirmedAt,
  });

  final int inputVersion;
  final int analysisVersion;
  final int confirmationVersion;
  final JobTargetCandidate candidate;
  final DateTime confirmedAt;
}

final class JobTarget {
  const JobTarget({
    required this.id,
    required this.userId,
    required this.input,
    required this.inputVersion,
    required this.stage,
    required this.createdAt,
    required this.updatedAt,
    this.analysis,
    this.confirmation,
  });

  final String id;
  final String userId;
  final JobTargetInput input;
  final int inputVersion;
  final JobTargetStage stage;
  final JobTargetAnalysis? analysis;
  final JobTargetConfirmation? confirmation;
  final DateTime createdAt;
  final DateTime updatedAt;
}

enum JobPreparationOperationStage {
  target,
  analysis,
  confirmation,
  profile,
  snapshot,
  goal,
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
  String toString() {
    return 'JobPreparationException(kind: ${kind.name}, '
        'stage: ${stage?.name})';
  }
}
