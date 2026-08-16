import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/interview/interview_resume_file.dart';
import 'package:speakup/features/coaching/preparation/preparation_client.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

abstract interface class JobPreparationClient implements PreparationClient {
  Future<InterviewPreparation> createInterviewPreparation({
    required InterviewPreparationInput input,
    InterviewResumeFile? resume,
    required String idempotencyKey,
  });

  Future<InterviewPreparation> getInterviewPreparation(
    String interviewPreparationId,
  );

  Future<InterviewPreparation> regenerateInterviewPreparation({
    required String interviewPreparationId,
    required int expectedVersion,
    required InterviewPreparationInput input,
    required String idempotencyKey,
  });

  Future<InterviewPreparation> confirmInterviewPreparation({
    required String interviewPreparationId,
    required int expectedVersion,
    required InterviewPreparationCandidate candidate,
    required String idempotencyKey,
  });

  Future<InterviewPreparation> discardInterviewPreparation({
    required String interviewPreparationId,
    required int expectedVersion,
    required String idempotencyKey,
  });

  Future<List<PracticePlanSummary>> listPlans({
    required PracticeExperience experience,
  });

  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  });
}
