import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/coaching/preparation/practice_plan_client_action.dart';
import 'package:speakup/features/coaching/ielts/ielts_assignment.dart';
import 'package:speakup/features/coaching/ielts/ielts_preparation_controller.dart';
import 'package:speakup/features/coaching/ielts/ielts_question_bank.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';
import 'package:speakup/features/coaching/scene/scene.dart';

typedef ClientActionPracticePlanReader =
    Future<PracticePlan> Function(String planId);
typedef ClientActionPracticePlanConfirmer =
    Future<PracticePlan> Function({
      required String planId,
      required int expectedVersion,
      required String idempotencyKey,
    });
typedef ClientActionPracticeSessionCreator =
    Future<PreparationPracticeBootstrap> Function({
      required PracticePlan plan,
      required CreatePreparationSessionInput input,
      required String idempotencyKey,
    });
typedef ClientActionIdFactory = String Function(String scope);

enum PracticePlanClientActionFailure { localExistingPractice }

final class PracticePlanClientActionController extends ChangeNotifier {
  PracticePlanClientActionController({
    required this.conversationController,
    required this.practiceController,
    required this.ieltsPreparationController,
    required this.workspaceController,
    required this.readPlan,
    required this.confirmPlan,
    required this.createSession,
    ClientActionIdFactory? idFactory,
  }) : _idFactory = idFactory ?? _secureClientActionId;

  final ConversationController conversationController;
  final PracticeController practiceController;
  final IeltsPreparationController ieltsPreparationController;
  final PracticeWorkspaceController workspaceController;
  final ClientActionPracticePlanReader readPlan;
  final ClientActionPracticePlanConfirmer confirmPlan;
  final ClientActionPracticeSessionCreator createSession;
  final ClientActionIdFactory _idFactory;

  _ConfirmationAttempt? _attempt;
  PracticePlanClientActionFailure? _failure;
  String? _errorMessage;
  bool _busy = false;
  bool _disposed = false;
  int _generation = 0;

  bool get isBusy => _busy;
  PracticePlanClientActionFailure? get failure => _failure;
  String? get errorMessage => _errorMessage;

  Future<IeltsPracticeAssignment> loadIELTSPreview(
    ConfirmPracticePlanClientAction action,
  ) async {
    if (action.practiceExperience !=
        PracticeExperience.ieltsSpeaking.wireValue) {
      throw ArgumentError.value(
        action.practiceExperience,
        'practiceExperience',
      );
    }
    final plan = await readPlan(action.practicePlanId);
    if (!_matchesPlan(action, plan) || plan.ieltsAssignment == null) {
      throw const _ClientActionConflict();
    }
    return plan.ieltsAssignment!;
  }

  Future<bool> confirm(
    ConfirmPracticePlanClientAction action, {
    bool replaceCurrentPractice = false,
  }) async {
    if (_disposed || _busy) {
      return false;
    }
    final generation = _generation;
    final existing = _attempt;
    var attempt = existing != null && existing.matches(action)
        ? existing
        : _ConfirmationAttempt(
            action: action,
            workspaceOperationId: _idFactory('client-action-workspace'),
            confirmationKey: _idFactory('client-action-plan-confirmation'),
            sessionKey: _idFactory('client-action-session'),
            voiceKey: _idFactory('client-action-voice'),
          );
    _attempt = attempt;
    _busy = true;
    _failure = null;
    _errorMessage = null;
    notifyListeners();
    try {
      final plan = await readPlan(action.practicePlanId);
      if (!_isCurrent(generation) || !_matchesPlan(action, plan)) {
        throw const _ClientActionConflict();
      }
      final ieltsSelection = _exactIeltsSelection(plan);
      if (attempt.lease == null) {
        if (plan.sourceThreadId == null ||
            conversationController.threadId != plan.sourceThreadId) {
          throw const _ClientActionConflict();
        }
      }
      if (attempt.lease == null &&
          workspaceController.hasResumableForPlan(plan.id)) {
        final outcome = await workspaceController
            .resumeCurrentPracticeWithOutcome();
        if (!_isCurrent(generation)) {
          return false;
        }
        if (outcome == PracticeWorkspaceResumeOutcome.resumed) {
          _attempt = null;
          _failure = null;
          _errorMessage = null;
          return true;
        }
        if (outcome == PracticeWorkspaceResumeOutcome.unavailable ||
            outcome == PracticeWorkspaceResumeOutcome.none) {
          throw const _ClientActionUnavailable();
        }
      }
      final lease = attempt.lease == null && replaceCurrentPractice
          ? await workspaceController.replaceCurrentPractice(
              attempt.workspaceOperationId,
            )
          : await workspaceController.acquirePractice(
              attempt.workspaceOperationId,
            );
      if (!_isCurrent(generation) || lease == null) {
        if (_isCurrent(generation) && workspaceController.hasResumable) {
          throw const _ClientActionExistingPractice();
        }
        throw const _ClientActionUnavailable();
      }
      attempt = attempt.withLease(lease);
      _attempt = attempt;

      final confirmedPlan = plan.status == PracticePlanStatus.ready
          ? plan
          : await confirmPlan(
              planId: plan.id,
              expectedVersion: action.planVersion,
              idempotencyKey: attempt.confirmationKey,
            );
      if (!_isCurrent(generation) ||
          !_matchesConfirmedPlan(action, confirmedPlan)) {
        throw const _ClientActionConflict();
      }
      final bootstrap = await createSession(
        plan: confirmedPlan,
        input: CreatePreparationSessionInput(
          expectedPlanVersion: confirmedPlan.version,
        ),
        idempotencyKey: attempt.sessionKey,
      );
      if (!_isCurrent(generation) ||
          bootstrap.session.planId != action.practicePlanId) {
        throw const _ClientActionConflict();
      }
      final committed = await workspaceController.commitSession(
        lease: lease,
        planId: bootstrap.session.planId,
        sessionId: bootstrap.session.id,
        scene: plan.sceneSelection.scene,
      );
      if (!_isCurrent(generation) || !committed) {
        throw const _ClientActionUnavailable();
      }
      await practiceController.activateCreatedPractice(
        scene: plan.sceneSelection.scene,
        sessionId: bootstrap.session.id,
        planId: bootstrap.session.planId,
        practiceMode: bootstrap.session.practiceMode,
        turnLimit: bootstrap.maxEffectiveTurns,
        clientOperationId: attempt.voiceKey,
      );
      if (ieltsSelection != null) {
        await ieltsPreparationController.beginSession(
          bootstrap.session.id,
          bootstrap.session.practiceMode,
          ieltsSelection,
        );
      }
      if (!_isCurrent(generation)) {
        return false;
      }
      _attempt = null;
      _failure = null;
      _errorMessage = null;
      return true;
    } on _ClientActionExistingPractice {
      if (_isCurrent(generation)) {
        _failure = PracticePlanClientActionFailure.localExistingPractice;
        _errorMessage = '当前已有进行中的练习。';
      }
      return false;
    } on _ClientActionConflict {
      final recovered = await _discardPendingAttempt(attempt);
      if (_isCurrent(generation)) {
        _attempt = null;
        _failure = null;
        _errorMessage = recovered
            ? '练习方案已经变化，请让 Agent 重新生成后再确认。'
            : workspaceController.errorMessage ?? '练习方案已失效，但练习准备记录暂时无法清理。';
      }
      return false;
    } on PreparationLaunchException catch (error) {
      if (_isCurrent(generation)) {
        if (_isStaleClientActionFailure(error)) {
          final recovered = await _discardPendingAttempt(attempt);
          if (!_isCurrent(generation)) {
            return false;
          }
          _attempt = null;
          _failure = null;
          _errorMessage = recovered
              ? '练习方案已经变化，请让 Agent 重新生成后再确认。'
              : workspaceController.errorMessage ?? '练习方案已失效，但练习准备记录暂时无法清理。';
        } else {
          _failure = null;
          _errorMessage = '练习暂时无法开始，请重试当前确认。';
        }
      }
      return false;
    } on Object {
      if (_isCurrent(generation)) {
        _failure = null;
        _errorMessage = workspaceController.errorMessage ?? '练习暂时无法开始，请重试当前确认。';
      }
      return false;
    } finally {
      if (_isCurrent(generation)) {
        _busy = false;
        notifyListeners();
      }
    }
  }

  Future<void> clearAccountState() async {
    _generation++;
    _attempt = null;
    _failure = null;
    _errorMessage = null;
    _busy = false;
    if (!_disposed) {
      notifyListeners();
    }
  }

  bool _isCurrent(int generation) => !_disposed && generation == _generation;

  Future<bool> _discardPendingAttempt(_ConfirmationAttempt attempt) {
    final lease = attempt.lease;
    return lease == null
        ? Future<bool>.value(true)
        : workspaceController.discardPendingPractice(lease);
  }

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    super.dispose();
  }
}

IeltsPracticeSelection? _exactIeltsSelection(PracticePlan plan) {
  if (plan.sceneSelection.scene.experience !=
      PracticeExperience.ieltsSpeaking) {
    return null;
  }
  final assignment = plan.ieltsAssignment;
  if (assignment == null || assignment.mode != plan.practiceOption.mode) {
    throw const _ClientActionConflict();
  }
  final selection = IeltsPracticeSelection(
    part1SetId: assignment.part(IeltsSpeakingPart.part1)?.sourceId,
    topicGroupId:
        assignment.part(IeltsSpeakingPart.part2)?.sourceId ??
        assignment.part(IeltsSpeakingPart.part3)?.sourceId,
  );
  if (!selection.isValidForMode(assignment.mode)) {
    throw const _ClientActionConflict();
  }
  return selection;
}

bool _isStaleClientActionFailure(PreparationLaunchException error) =>
    error.kind == PreparationLaunchFailureKind.notFound ||
    (error.kind == PreparationLaunchFailureKind.conflict &&
        error.errorCode == 'version_conflict');

final class _ConfirmationAttempt {
  const _ConfirmationAttempt({
    required this.action,
    required this.workspaceOperationId,
    required this.confirmationKey,
    required this.sessionKey,
    required this.voiceKey,
    this.lease,
  });

  final ConfirmPracticePlanClientAction action;
  final String workspaceOperationId;
  final String confirmationKey;
  final String sessionKey;
  final String voiceKey;
  final PracticeWorkspaceLease? lease;

  bool matches(ConfirmPracticePlanClientAction candidate) =>
      action.practicePlanId == candidate.practicePlanId &&
      action.planVersion == candidate.planVersion;

  _ConfirmationAttempt withLease(PracticeWorkspaceLease value) =>
      _ConfirmationAttempt(
        action: action,
        workspaceOperationId: workspaceOperationId,
        confirmationKey: confirmationKey,
        sessionKey: sessionKey,
        voiceKey: voiceKey,
        lease: value,
      );
}

bool _matchesPlan(ConfirmPracticePlanClientAction action, PracticePlan plan) {
  if (plan.id != action.practicePlanId ||
      !((plan.status == PracticePlanStatus.draft &&
              plan.version == action.planVersion) ||
          (plan.status == PracticePlanStatus.ready &&
              plan.version == action.planVersion + 1)) ||
      plan.sceneSelection.scene.name != action.sceneName ||
      plan.sceneSelection.scene.experience.wireValue !=
          action.practiceExperience ||
      plan.sceneSelection.scene.category.wireValue != action.sceneCategory ||
      plan.practiceOption.mode.wireValue != action.practiceMode ||
      plan.practiceOption.displayName != action.practiceScope ||
      Duration(seconds: plan.sessionPolicy.suggestedDurationSeconds) !=
          action.suggestedDuration ||
      plan.sessionPolicy.minEffectiveTurns != action.minEffectiveTurns ||
      plan.sessionPolicy.maxEffectiveTurns != action.maxEffectiveTurns) {
    return false;
  }
  final target = plan.sceneSelection.scene.prompt.practiceGoal;
  final roles = plan.selectedRoles.map((role) => role.displayName).toList();
  return target == action.target && listEquals(roles, action.roles);
}

bool _matchesConfirmedPlan(
  ConfirmPracticePlanClientAction action,
  PracticePlan plan,
) =>
    plan.status == PracticePlanStatus.ready &&
    plan.version == action.planVersion + 1 &&
    _matchesPlan(action, plan);

final class _ClientActionConflict implements Exception {
  const _ClientActionConflict();
}

final class _ClientActionUnavailable implements Exception {
  const _ClientActionUnavailable();
}

final class _ClientActionExistingPractice implements Exception {
  const _ClientActionExistingPractice();
}

final _clientActionRandom = Random.secure();

String _secureClientActionId(String scope) {
  final value = StringBuffer('${scope}_');
  for (var index = 0; index < 16; index++) {
    value.write(
      _clientActionRandom.nextInt(256).toRadixString(16).padLeft(2, '0'),
    );
  }
  return value.toString();
}
