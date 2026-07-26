import 'package:flutter/material.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/practice/practice.dart';
import 'package:speakup/features/preparation/preparation.dart';
import 'package:speakup/features/review/review.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/auth_gate.dart';

class SpeakUpApp extends StatelessWidget {
  const SpeakUpApp({required AuthController authController, super.key})
    : _authentication = (controller: authController);

  const SpeakUpApp.preview({super.key}) : _authentication = null;

  final ({AuthController controller})? _authentication;

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
          ? const _AuthenticatedNavigator()
          : AuthGate(
              controller: controller,
              authenticatedBuilder: (_, _) => const _AuthenticatedNavigator(),
            ),
    );
  }
}

class _AuthenticatedNavigator extends StatelessWidget {
  const _AuthenticatedNavigator();

  @override
  Widget build(BuildContext context) {
    return Navigator(
      initialRoute: AppRoutes.home,
      onGenerateRoute: (settings) {
        final page = switch (settings.name) {
          AppRoutes.home => const SpeakUpShell(),
          AppRoutes.preparation => const PreparationPage(showBackButton: true),
          AppRoutes.practice => const PracticePage(),
          AppRoutes.conversation => const SpeakUpShell(showBackButton: true),
          AppRoutes.review => const ReviewPage(showBackButton: true),
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
