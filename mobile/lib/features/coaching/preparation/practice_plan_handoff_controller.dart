import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/features/agent/handoff/agent_handoff.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/preparation/preparation_launch_models.dart';
import 'package:speakup/features/coaching/preparation/preparation_models.dart';
import 'package:speakup/features/coaching/preparation/practice_workspace_controller.dart';

typedef HandoffPracticePlanReader =
    Future<PracticePlan> Function(String planId);
typedef HandoffPracticePlanConfirmer =
    Future<PreparationPracticeBootstrap> Function({
      required PracticePlan plan,
      required CreatePreparationSessionInput input,
      required String idempotencyKey,
    });
typedef HandoffIdFactory = String Function(String scope);

final class PracticePlanHandoffController extends ChangeNotifier {
  PracticePlanHandoffController({
    required this.conversationController,
    required this.practiceController,
    required this.workspaceController,
    required this.readPlan,
    required this.confirmPlan,
    HandoffIdFactory? idFactory,
  }) : _idFactory = idFactory ?? _secureHandoffId;

  final ConversationController conversationController;
  final PracticeController practiceController;
  final PracticeWorkspaceController workspaceController;
  final HandoffPracticePlanReader readPlan;
  final HandoffPracticePlanConfirmer confirmPlan;
  final HandoffIdFactory _idFactory;

  _ConfirmationAttempt? _attempt;
  String? _errorMessage;
  bool _busy = false;
  bool _disposed = false;
  int _generation = 0;

  bool get isBusy => _busy;
  String? get errorMessage => _errorMessage;

  Future<bool> confirm(ConfirmPracticePlanHandoff handoff) async {
    if (_disposed || _busy) {
      return false;
    }
    final generation = _generation;
    final existing = _attempt;
    var attempt = existing != null && existing.matches(handoff)
        ? existing
        : _ConfirmationAttempt(
            handoff: handoff,
            workspaceOperationId: _idFactory('handoff-workspace'),
            sessionKey: _idFactory('handoff-session'),
            voiceKey: _idFactory('handoff-voice'),
          );
    _attempt = attempt;
    _busy = true;
    _errorMessage = null;
    notifyListeners();
    try {
      final plan = await readPlan(handoff.practicePlanId);
      if (!_isCurrent(generation) || !_matchesPlan(handoff, plan)) {
        throw const _HandoffConflict();
      }
      if (attempt.lease == null) {
        if (plan.sourceThreadId == null ||
            conversationController.threadId != plan.sourceThreadId) {
          throw const _HandoffConflict();
        }
      }
      final lease = await workspaceController.acquireThread(
        attempt.workspaceOperationId,
      );
      if (!_isCurrent(generation) || lease == null) {
        throw const _HandoffUnavailable();
      }
      attempt = attempt.withLease(lease);
      _attempt = attempt;

      final bootstrap = await confirmPlan(
        plan: plan,
        input: CreatePreparationSessionInput(
          expectedPlanRevision: handoff.planRevision,
          userConfirmed: true,
        ),
        idempotencyKey: attempt.sessionKey,
      );
      if (!_isCurrent(generation) ||
          bootstrap.session.planId != handoff.practicePlanId) {
        throw const _HandoffConflict();
      }
      final committed = await workspaceController.commitSession(
        lease: lease,
        goalId: null,
        sessionId: bootstrap.session.id,
        scene: plan.sceneSelection.scene,
      );
      if (!_isCurrent(generation) || !committed) {
        throw const _HandoffUnavailable();
      }
      await practiceController.activateCreatedPractice(
        scene: plan.sceneSelection.scene,
        sessionId: bootstrap.session.id,
        planId: bootstrap.session.planId,
        practiceMode: bootstrap.session.practiceMode,
        turnLimit: bootstrap.maxEffectiveTurns,
        clientOperationId: attempt.voiceKey,
      );
      if (!_isCurrent(generation)) {
        return false;
      }
      _attempt = null;
      _errorMessage = null;
      return true;
    } on _HandoffConflict {
      if (_isCurrent(generation)) {
        _attempt = null;
        _errorMessage = '练习方案已经变化，请让 Agent 重新生成后再确认。';
      }
      return false;
    } on PreparationLaunchException catch (error) {
      if (_isCurrent(generation)) {
        if (_isStaleHandoffFailure(error)) {
          _attempt = null;
          _errorMessage = '练习方案已经变化，请让 Agent 重新生成后再确认。';
        } else if (error.errorCode == 'active_session_conflict') {
          _errorMessage = '当前已有进行中的练习，请先完成或结束后再确认。';
        } else {
          _errorMessage = '练习暂时无法开始，请重试当前确认。';
        }
      }
      return false;
    } on Object {
      if (_isCurrent(generation)) {
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
    _errorMessage = null;
    _busy = false;
    if (!_disposed) {
      notifyListeners();
    }
  }

  bool _isCurrent(int generation) => !_disposed && generation == _generation;

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    super.dispose();
  }
}

bool _isStaleHandoffFailure(PreparationLaunchException error) =>
    error.kind == PreparationLaunchFailureKind.notFound ||
    (error.kind == PreparationLaunchFailureKind.conflict &&
        error.errorCode == 'version_conflict');

final class _ConfirmationAttempt {
  const _ConfirmationAttempt({
    required this.handoff,
    required this.workspaceOperationId,
    required this.sessionKey,
    required this.voiceKey,
    this.lease,
  });

  final ConfirmPracticePlanHandoff handoff;
  final String workspaceOperationId;
  final String sessionKey;
  final String voiceKey;
  final PracticeWorkspaceLease? lease;

  bool matches(ConfirmPracticePlanHandoff candidate) =>
      handoff.practicePlanId == candidate.practicePlanId &&
      handoff.planRevision == candidate.planRevision;

  _ConfirmationAttempt withLease(PracticeWorkspaceLease value) =>
      _ConfirmationAttempt(
        handoff: handoff,
        workspaceOperationId: workspaceOperationId,
        sessionKey: sessionKey,
        voiceKey: voiceKey,
        lease: value,
      );
}

bool _matchesPlan(ConfirmPracticePlanHandoff handoff, PracticePlan plan) {
  if (plan.id != handoff.practicePlanId ||
      plan.revision != handoff.planRevision ||
      plan.status != PracticePlanStatus.ready ||
      plan.sceneSelection.scene.name != handoff.sceneName ||
      plan.sceneSelection.scene.experience.wireValue !=
          handoff.practiceExperience ||
      plan.sceneSelection.scene.category.wireValue != handoff.sceneCategory ||
      plan.practiceOption.mode.wireValue != handoff.practiceMode ||
      plan.practiceOption.displayName != handoff.practiceScope ||
      Duration(seconds: plan.sessionPolicy.suggestedDurationSeconds) !=
          handoff.suggestedDuration ||
      plan.sessionPolicy.minEffectiveTurns != handoff.minEffectiveTurns ||
      plan.sessionPolicy.maxEffectiveTurns != handoff.maxEffectiveTurns) {
    return false;
  }
  final target =
      plan.goalSnapshot?.title ?? plan.sceneSelection.scene.prompt.practiceGoal;
  final roles = plan.selectedRoles.map((role) => role.displayName).toList();
  return target == handoff.target && listEquals(roles, handoff.roles);
}

final class _HandoffConflict implements Exception {
  const _HandoffConflict();
}

final class _HandoffUnavailable implements Exception {
  const _HandoffUnavailable();
}

final _handoffRandom = Random.secure();

String _secureHandoffId(String scope) {
  final value = StringBuffer('${scope}_');
  for (var index = 0; index < 16; index++) {
    value.write(_handoffRandom.nextInt(256).toRadixString(16).padLeft(2, '0'));
  }
  return value.toString();
}
