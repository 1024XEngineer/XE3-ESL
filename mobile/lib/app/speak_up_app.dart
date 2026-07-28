import 'dart:async';

import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/practice/practice.dart';
import 'package:speakup/features/preparation/job_preparation_controller.dart';
import 'package:speakup/features/preparation/job_preparation_wizard.dart';
import 'package:speakup/features/preparation/preparation.dart';
import 'package:speakup/features/preparation/preparation_controller.dart';
import 'package:speakup/features/preparation/preparation_launch_controller.dart';
import 'package:speakup/features/review/review.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/auth_gate.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/review/review_history_controller.dart';

class SpeakUpApp extends StatelessWidget {
  const SpeakUpApp({
    required AuthController authController,
    required this.agentController,
    required this.preparationController,
    this.jobPreparationController,
    this.preparationLaunchController,
    this.reviewHistoryController,
    super.key,
  }) : _authentication = (controller: authController),
       _allowFakePreview = false;

  const SpeakUpApp.preview({
    this.agentController,
    this.preparationController,
    this.jobPreparationController,
    this.preparationLaunchController,
    this.reviewHistoryController,
    super.key,
  }) : _authentication = null,
       _allowFakePreview = true;

  final ({AuthController controller})? _authentication;
  final AgentController? agentController;
  final PreparationController? preparationController;
  final JobPreparationController? jobPreparationController;
  final PreparationLaunchController? preparationLaunchController;
  final ReviewHistoryController? reviewHistoryController;
  final bool _allowFakePreview;

  @override
  Widget build(BuildContext context) {
    final controller = _authentication?.controller;
    return MaterialApp(
      title: 'SpeakUp',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFF4F5054),
          surface: const Color(0xFFF3F3F0),
        ),
        scaffoldBackgroundColor: const Color(0xFFF3F3F0),
        textTheme: ThemeData.light().textTheme.apply(
          bodyColor: const Color(0xFF111217),
          displayColor: const Color(0xFF111217),
        ),
        useMaterial3: true,
      ),
      home: controller == null
          ? _AuthenticatedNavigator(
              agentController: agentController,
              preparationController: preparationController,
              jobPreparationController: jobPreparationController,
              preparationLaunchController: preparationLaunchController,
              reviewHistoryController: reviewHistoryController,
              allowFakePreview: _allowFakePreview,
            )
          : AuthGate(
              controller: controller,
              authenticatedBuilder: (_, user) => _AuthenticatedNavigator(
                user: user,
                authController: controller,
                agentController: agentController,
                preparationController: preparationController,
                jobPreparationController: jobPreparationController,
                preparationLaunchController: preparationLaunchController,
                reviewHistoryController: reviewHistoryController,
                allowFakePreview: _allowFakePreview,
              ),
            ),
    );
  }
}

class _AuthenticatedNavigator extends StatefulWidget {
  const _AuthenticatedNavigator({
    this.user,
    this.authController,
    this.agentController,
    this.preparationController,
    this.jobPreparationController,
    this.preparationLaunchController,
    this.reviewHistoryController,
    required this.allowFakePreview,
  });

  final User? user;
  final AuthController? authController;
  final AgentController? agentController;
  final PreparationController? preparationController;
  final JobPreparationController? jobPreparationController;
  final PreparationLaunchController? preparationLaunchController;
  final ReviewHistoryController? reviewHistoryController;
  final bool allowFakePreview;

  @override
  State<_AuthenticatedNavigator> createState() =>
      _AuthenticatedNavigatorState();
}

class _AuthenticatedNavigatorState extends State<_AuthenticatedNavigator> {
  final _navigatorKey = GlobalKey<NavigatorState>();
  late final AgentController _agentController;
  late final bool _ownsAgentController;

  @override
  void initState() {
    super.initState();
    final injectedController = widget.agentController;
    if (injectedController == null && !widget.allowFakePreview) {
      throw StateError(
        'Production SpeakUpApp requires an injected AgentController.',
      );
    }
    _ownsAgentController = injectedController == null;
    _agentController =
        injectedController ?? AgentController(client: FakeAgentClient());
    _agentController.initialize();
    final user = widget.user;
    if (user != null) {
      unawaited(widget.jobPreparationController?.activateAccount(user.id));
    }
  }

  @override
  void didUpdateWidget(covariant _AuthenticatedNavigator oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.user?.id != widget.user?.id ||
        oldWidget.jobPreparationController != widget.jobPreparationController) {
      final user = widget.user;
      if (user != null) {
        unawaited(widget.jobPreparationController?.activateAccount(user.id));
      }
    }
  }

  @override
  void dispose() {
    if (_ownsAgentController) {
      _agentController.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Navigator(
      key: _navigatorKey,
      initialRoute: AppRoutes.home,
      onGenerateRoute: (settings) {
        final page = switch (settings.name) {
          AppRoutes.home => SpeakUpShell(
            previewMode: widget.allowFakePreview,
            user: widget.user,
            authController: widget.authController,
            agentController: _agentController,
            preparationController: widget.preparationController,
            jobPreparationController: widget.jobPreparationController,
            preparationLaunchController: widget.preparationLaunchController,
            reviewHistoryController: widget.reviewHistoryController,
          ),
          AppRoutes.preparation => PreparationPage(
            showBackButton: true,
            previewMode: widget.allowFakePreview,
            agentController: _agentController,
            preparationController: widget.preparationController,
            launchController: widget.preparationLaunchController,
            onPracticeStarted: () => _navigatorKey.currentState
                ?.pushReplacementNamed(AppRoutes.practice),
          ),
          AppRoutes.jobPreparation
              when widget.jobPreparationController != null =>
            JobPreparationWizard(
              controller: widget.jobPreparationController!,
              catalogController: widget.preparationController,
              onPracticeStarted: () => _navigatorKey.currentState
                  ?.pushReplacementNamed(AppRoutes.practice),
            ),
          AppRoutes.practice => PracticePage(
            previewMode: widget.allowFakePreview,
            agentController: _agentController,
          ),
          AppRoutes.conversation => SpeakUpShell(
            showBackButton: true,
            previewMode: widget.allowFakePreview,
            user: widget.user,
            authController: widget.authController,
            agentController: _agentController,
            preparationController: widget.preparationController,
            jobPreparationController: widget.jobPreparationController,
            preparationLaunchController: widget.preparationLaunchController,
            reviewHistoryController: widget.reviewHistoryController,
          ),
          AppRoutes.review => ReviewPage(
            showBackButton: true,
            previewMode: widget.allowFakePreview,
            practiceAvailable: _agentController.supportsPracticeFlow,
            historyController: widget.reviewHistoryController,
            agentController: _agentController,
          ),
          _ => null,
        };
        if (page == null) {
          return null;
        }
        return MaterialPageRoute<void>(
          settings: settings,
          builder: (_) => page,
        );
      },
    );
  }
}
