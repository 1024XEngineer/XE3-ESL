import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/interview/interview_resume_file.dart';
import 'package:speakup/features/coaching/interview/job_preparation_client.dart';
import 'package:speakup/features/coaching/interview/job_preparation_controller.dart';
import 'package:speakup/features/coaching/interview/job_preparation_models.dart';
import 'package:speakup/features/coaching/preparation/practice_launch_record_store.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

import '../../support/preparation_contract_fixtures.dart';

void main() {
  test(
    'Interview plus Resume uses one Preparation, one Plan, and one Session',
    () async {
      final harness = await _Harness.create();
      addTearDown(harness.dispose);
      harness.controller.updateInput(contractInterviewInput);
      harness.controller.selectResume(
        const InterviewResumeFile(
          name: 'backend-resume.pdf',
          bytes: <int>[0x25, 0x50, 0x44, 0x46, 0x2d],
        ),
      );

      final started = await harness.controller.createAndStartPractice();

      expect(started, isTrue);
      expect(harness.client.createdInputs, <InterviewPreparationInput>[
        contractInterviewInput,
      ]);
      expect(harness.client.createdResume?.name, 'backend-resume.pdf');
      expect(harness.client.confirmedVersions, <int>[1]);
      expect(
        harness.client.planInputs.single.interviewPreparationId,
        contractInterviewId,
      );
      expect(harness.client.planInputs.single.expectedInterviewVersion, 2);
      expect(harness.client.sessionInputs.single.expectedPlanVersion, 1);
      expect(harness.voiceActivations, <String>[contractSessionId]);
      expect(harness.workspace.currentSessionId, contractSessionId);
    },
  );

  test(
    'saved Plan opens directly without regenerating Interview Preparation',
    () async {
      final harness = await _Harness.create();
      addTearDown(harness.dispose);

      expect(await harness.controller.openSavedPlan(contractPlanId), isTrue);
      expect(harness.controller.openedSavedPlan, isTrue);
      expect(harness.controller.plan?.id, contractPlanId);
      expect(harness.client.createdInputs, isEmpty);
      expect(await harness.controller.startPractice(), isTrue);
      expect(harness.client.sessionInputs.single.expectedPlanVersion, 1);
    },
  );

  test('reopening the same saved Plan resumes its parked Session', () async {
    final harness = await _Harness.create();
    addTearDown(harness.dispose);

    expect(await harness.controller.openSavedPlan(contractPlanId), isTrue);
    expect(await harness.controller.startPractice(), isTrue);
    expect(await harness.workspace.parkCurrentPractice(), isTrue);
    expect(harness.practice.hasActivePractice, isFalse);

    expect(await harness.controller.openSavedPlan(contractPlanId), isTrue);
    expect(await harness.controller.startPractice(), isTrue);

    expect(harness.practice.practiceSessionId, contractSessionId);
    expect(harness.practice.practicePlanId, contractPlanId);
    expect(harness.client.sessionInputs, hasLength(1));
    expect(harness.voiceActivations, <String>[contractSessionId]);
  });

  test(
    'voice activation retry keeps the created Session and idempotency key',
    () async {
      final harness = await _Harness.create(voiceActivationFailures: 1);
      addTearDown(harness.dispose);

      expect(await harness.controller.openSavedPlan(contractPlanId), isTrue);
      expect(await harness.controller.startPractice(), isFalse);
      expect(harness.client.sessionInputs, hasLength(1));
      expect(harness.voiceActivationKeys, hasLength(1));

      expect(await harness.controller.retry(), isTrue);

      expect(harness.client.sessionInputs, hasLength(1));
      expect(harness.voiceActivationKeys, hasLength(2));
      expect(harness.voiceActivationKeys.toSet(), hasLength(1));
      expect(harness.practice.practiceSessionId, contractSessionId);
      expect(harness.practice.practicePlanId, contractPlanId);
    },
  );
}

final class _Harness {
  _Harness._({
    required this.practice,
    required this.workspace,
    required this.client,
    required this.controller,
    required this.voiceActivations,
    required this.voiceActivationKeys,
  });

  final PracticeController practice;
  final PracticeWorkspaceController workspace;
  final _FakeJobPreparationClient client;
  final JobPreparationController controller;
  final List<String> voiceActivations;
  final List<String> voiceActivationKeys;

  static Future<_Harness> create({int voiceActivationFailures = 0}) async {
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
    final client = _FakeJobPreparationClient();
    final voiceActivations = <String>[];
    final voiceActivationKeys = <String>[];
    var remainingVoiceActivationFailures = voiceActivationFailures;
    final controller = JobPreparationController(
      client: client,
      workspaceController: workspace,
      idFactory: (scope) => '$scope-contract-key',
      voiceActivator:
          ({
            required scene,
            required bootstrap,
            required clientOperationId,
          }) async {
            voiceActivations.add(bootstrap.session.id);
            voiceActivationKeys.add(clientOperationId);
            if (remainingVoiceActivationFailures > 0) {
              remainingVoiceActivationFailures--;
              throw StateError('Voice activation failed.');
            }
            await practice.activateCreatedPractice(
              scene: scene,
              sessionId: bootstrap.session.id,
              planId: bootstrap.session.planId,
              practiceMode: bootstrap.session.practiceMode,
              turnLimit: bootstrap.maxEffectiveTurns,
              clientOperationId: clientOperationId,
            );
          },
    );
    return _Harness._(
      practice: practice,
      workspace: workspace,
      client: client,
      controller: controller,
      voiceActivations: voiceActivations,
      voiceActivationKeys: voiceActivationKeys,
    );
  }

  void dispose() {
    controller.dispose();
    workspace.dispose();
    practice.dispose();
  }
}

final class _FakeJobPreparationClient implements JobPreparationClient {
  final List<InterviewPreparationInput> createdInputs =
      <InterviewPreparationInput>[];
  InterviewResumeFile? createdResume;
  final List<int> confirmedVersions = <int>[];
  final List<CreatePracticePlanInput> planInputs = <CreatePracticePlanInput>[];
  final List<CreatePreparationSessionInput> sessionInputs =
      <CreatePreparationSessionInput>[];

  @override
  Future<InterviewPreparation> createInterviewPreparation({
    required InterviewPreparationInput input,
    InterviewResumeFile? resume,
    required String idempotencyKey,
  }) async {
    createdInputs.add(input);
    createdResume = resume;
    return contractInterviewPreparation();
  }

  @override
  Future<InterviewPreparation> confirmInterviewPreparation({
    required String interviewPreparationId,
    required int expectedVersion,
    required InterviewPreparationCandidate candidate,
    required String idempotencyKey,
  }) async {
    confirmedVersions.add(expectedVersion);
    return contractInterviewPreparation(
      status: InterviewPreparationStatus.confirmed,
      version: 2,
    );
  }

  @override
  Future<PracticePlan> createPlan({
    required CreatePracticePlanInput input,
    required String idempotencyKey,
  }) async {
    planInputs.add(input);
    return contractPlan(includeInterview: true);
  }

  @override
  Future<PreparationPracticeBootstrap> createSession({
    required PracticePlan plan,
    required CreatePreparationSessionInput input,
    required String idempotencyKey,
  }) async {
    sessionInputs.add(input);
    return contractBootstrap(plan);
  }

  @override
  Future<PracticePlan> getPlan(String planId) async =>
      contractPlan(includeInterview: true);

  @override
  Future<List<PracticePlanSummary>> listPlans({
    required PracticeExperience experience,
  }) async => <PracticePlanSummary>[];

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
}
