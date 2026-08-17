import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/interview/interview_resume_file.dart';
import 'package:speakup/features/coaching/interview/job_preparation_client.dart';
import 'package:speakup/features/coaching/interview/job_preparation_controller.dart';
import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/interview/job_preparation_wizard.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/preparation_contract_fixtures.dart';

void main() {
  testWidgets(
    'wizard starts the aggregate flow without legacy draft settings',
    (tester) async {
      final practice = PracticeController(
        client: FakePracticeClient(
          planId: contractPlanId,
          practiceExperience: PracticeExperience.interview,
          sceneCategory: SceneCategory.interviewProfessional,
          turnLimit: 6,
        ),
      );
      final workspace = PracticeWorkspaceController(
        practiceController: practice,
        recordStore: MemoryPracticeLaunchRecordStore(),
      );
      await workspace.activateAccount(contractUserId);
      final client = _WizardClient();
      var started = 0;
      final controller = JobPreparationController(
        client: client,
        workspaceController: workspace,
        idFactory: (scope) => '$scope-contract-key',
        voiceActivator:
            ({
              required scene,
              required bootstrap,
              required clientOperationId,
            }) async {},
      );
      addTearDown(() {
        controller.dispose();
        workspace.dispose();
        practice.dispose();
      });

      await tester.pumpWidget(
        MaterialApp(
          home: JobPreparationWizard(
            controller: controller,
            onPracticeStarted: () => started++,
          ),
        ),
      );
      await tester.enterText(
        find.byKey(const Key('job-input-field')),
        'Responsibilities:\n${contractInterviewInput.jobDescription!}',
      );
      await tester.tap(
        find.byKey(const Key('create-and-start-interview-button')),
      );
      await tester.pumpAndSettle();

      expect(started, 1);
      expect(client.preparationCreates, 1);
      expect(client.planCreates, 1);
      expect(client.sessionCreates, 1);
      expect(find.byKey(const Key('job-draft-card')), findsNothing);
      expect(find.byKey(const Key('job-plan-advanced')), findsNothing);
    },
  );
}

final class _WizardClient implements JobPreparationClient {
  int preparationCreates = 0;
  int planCreates = 0;
  int sessionCreates = 0;
  InterviewPreparationInput? _input;

  @override
  Future<InterviewPreparation> createInterviewPreparation({
    required InterviewPreparationInput input,
    InterviewResumeFile? resume,
    required String idempotencyKey,
  }) async {
    preparationCreates++;
    _input = input;
    return _preparation(InterviewPreparationStatus.draft, 1);
  }

  @override
  Future<InterviewPreparation> confirmInterviewPreparation({
    required String interviewPreparationId,
    required int expectedVersion,
    required InterviewPreparationCandidate candidate,
    required String idempotencyKey,
  }) async => _preparation(InterviewPreparationStatus.confirmed, 2);

  @override
  Future<PracticePlan> createPlan({
    required CreatePracticePlanInput input,
    required String idempotencyKey,
  }) async {
    planCreates++;
    return contractPlan(includeInterview: true);
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    sessionCreates++;
    return contractBootstrap(plan);
  }

  @override
  Future<PracticePlan> getPlan(String planId) async =>
      contractPlan(includeInterview: true);

  @override
  Future<List<PracticePlanSummary>> listPlans({
    required PracticeExperience experience,
  }) async => const <PracticePlanSummary>[];

  @override
  Future<void> deletePlan(String planId) async {}

  @override
  Future<InterviewPreparation> getInterviewPreparation(String id) async =>
      contractInterviewPreparation();

  @override
  Future<InterviewPreparation> regenerateInterviewPreparation({
    required String interviewPreparationId,
    required int expectedVersion,
    required InterviewPreparationInput input,
    required String idempotencyKey,
  }) async => contractInterviewPreparation();

  @override
  Future<InterviewPreparation> discardInterviewPreparation({
    required String interviewPreparationId,
    required int expectedVersion,
    required String idempotencyKey,
  }) async => contractInterviewPreparation(
    status: InterviewPreparationStatus.discarded,
    version: expectedVersion + 1,
  );

  @override
  Future<PracticePlan> confirmPlan({
    required String planId,
    required int expectedVersion,
    required String idempotencyKey,
  }) async => contractPlan();

  @override
  Future<void> clearAccountState() async {}

  InterviewPreparation _preparation(
    InterviewPreparationStatus status,
    int version,
  ) => InterviewPreparation(
    id: contractInterviewId,
    userId: contractUserId,
    input: _input!,
    candidate: contractCandidate,
    status: status,
    version: version,
    createdAt: DateTime.parse(contractCreatedAt),
    updatedAt: DateTime.parse(contractCreatedAt),
  );
}
