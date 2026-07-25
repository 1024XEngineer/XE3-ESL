import 'package:flutter/material.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/practice/practice.dart';
import 'package:speakup/features/preparation/preparation.dart';
import 'package:speakup/features/review/review.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/auth_gate.dart';
import 'package:speakup/identity/model/identity_models.dart';

class SpeakUpApp extends StatelessWidget {
  const SpeakUpApp({
    required AuthController authController,
    required this.agentController,
    super.key,
  }) : _authentication = (controller: authController),
       _allowFakePreview = false;

  const SpeakUpApp.preview({this.agentController, super.key})
    : _authentication = null,
      _allowFakePreview = true;

  final ({AuthController controller})? _authentication;
  final AgentController? agentController;
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
              allowFakePreview: _allowFakePreview,
            )
          : AuthGate(
              controller: controller,
              authenticatedBuilder: (_, user) => _AuthenticatedNavigator(
                user: user,
                authController: controller,
                agentController: agentController,
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
    required this.allowFakePreview,
  });

  final User? user;
  final AuthController? authController;
  final AgentController? agentController;
  final bool allowFakePreview;

  @override
  State<_AuthenticatedNavigator> createState() =>
      _AuthenticatedNavigatorState();
}

class _AuthenticatedNavigatorState extends State<_AuthenticatedNavigator> {
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
      initialRoute: AppRoutes.home,
      onGenerateRoute: (settings) {
        final page = switch (settings.name) {
          AppRoutes.home => SpeakUpShell(
            previewMode: widget.allowFakePreview,
            user: widget.user,
            authController: widget.authController,
            agentController: _agentController,
          ),
          AppRoutes.preparation => PreparationPage(
            showBackButton: true,
            previewMode: widget.allowFakePreview,
            agentController: _agentController,
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
          ),
          AppRoutes.review => ReviewPage(
            showBackButton: true,
            previewMode: widget.allowFakePreview,
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
