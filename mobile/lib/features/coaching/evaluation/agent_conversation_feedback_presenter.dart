import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:speakup/design/practice_conversation_components.dart';
import 'package:speakup/features/agent/conversation/agent_models.dart';
import 'package:speakup/features/agent/conversation/conversation.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback.dart';
import 'package:speakup/features/coaching/evaluation/turn_feedback_controller.dart';

/// Adapts Coaching feedback to the narrow presentation port owned by Agent UI.
final class AgentConversationFeedbackPresenter extends ChangeNotifier
    implements ConversationMessageFeedbackPresenter {
  AgentConversationFeedbackPresenter({required this.controller}) {
    controller.addListener(_relay);
  }

  final SpeechFeedbackController controller;
  final Map<String, String> _sources = <String, String>{};
  String? _threadId;
  bool _disposed = false;

  @override
  void syncMessages({
    required String? threadId,
    required List<AgentMessage> messages,
  }) {
    if (_disposed) {
      return;
    }
    _threadId = threadId;
    final current = <String, String>{};
    for (final message in messages) {
      final statusUrl = message.speechFeedbackStatusUrl;
      if (statusUrl != null) {
        current[_sourceKey(threadId, message.id)] = statusUrl;
      }
    }
    for (final entry in _sources.entries.toList()) {
      if (current[entry.key] != entry.value) {
        controller.removeSource(entry.key);
        _sources.remove(entry.key);
      }
    }
    for (final entry in current.entries) {
      if (_sources[entry.key] == entry.value) {
        continue;
      }
      _sources[entry.key] = entry.value;
      unawaited(controller.load(sourceKey: entry.key, statusUrl: entry.value));
    }
  }

  @override
  void clearMessages() {
    for (final sourceKey in _sources.keys) {
      controller.removeSource(sourceKey);
    }
    _sources.clear();
    _threadId = null;
  }

  @override
  InlineLanguageSuggestion? correctionFor(AgentMessage message) {
    final items = _projection(message)?.feedback?.items;
    final item = items?.correction;
    if (item?.suggestedText == null) {
      return null;
    }
    return InlineLanguageSuggestion(
      text: item!.suggestedText!,
      originalText: item.anchor.originalExcerpt,
      explanation: item.explanation,
    );
  }

  @override
  InlineLanguageSuggestion? polishFor(AgentMessage message) {
    final items = _projection(message)?.feedback?.items;
    final item = items?.polish;
    if (item?.suggestedText == null) {
      return null;
    }
    return InlineLanguageSuggestion(
      text: item!.suggestedText!,
      explanation: item.explanation,
    );
  }

  SpeechFeedbackProjection? _projection(AgentMessage message) {
    if (message.speechFeedbackStatusUrl == null) {
      return null;
    }
    final projection = controller.projectionFor(
      _sourceKey(_threadId, message.id),
    );
    if (projection?.feedback?.scoreabilityStatus ==
        SpeechFeedbackScoreabilityStatus.insufficient) {
      return null;
    }
    return projection;
  }

  void _relay() {
    if (!_disposed) {
      notifyListeners();
    }
  }

  @override
  void dispose() {
    _disposed = true;
    controller.removeListener(_relay);
    clearMessages();
    super.dispose();
  }
}

String _sourceKey(String? threadId, String messageId) =>
    'agent:${threadId ?? 'draft'}:$messageId';
