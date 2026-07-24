import 'package:flutter/material.dart';
import 'package:speakup/app/app_routes.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/conversation/conversation.dart';
import 'package:speakup/features/practice/practice.dart';
import 'package:speakup/features/preparation/preparation.dart';
import 'package:speakup/features/review/review.dart';

class SpeakUpApp extends StatelessWidget {
  const SpeakUpApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'SpeakUp',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFF767BF2),
          surface: const Color(0xFFF8F8FC),
        ),
        scaffoldBackgroundColor: const Color(0xFFF8F8FC),
        textTheme: ThemeData.light().textTheme.apply(
          bodyColor: const Color(0xFF111217),
          displayColor: const Color(0xFF111217),
        ),
        useMaterial3: true,
      ),
      initialRoute: AppRoutes.home,
      routes: {
        AppRoutes.home: (_) => const SpeakUpShell(),
        AppRoutes.preparation: (_) => const PreparationPage(),
        AppRoutes.practice: (_) => const PracticePage(),
        AppRoutes.conversation: (_) => const ConversationPage(),
        AppRoutes.review: (_) => const ReviewPage(),
      },
    );
  }
}
