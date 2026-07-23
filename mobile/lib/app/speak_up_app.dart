import 'package:flutter/material.dart';
import 'package:speakup/features/conversation/conversation.dart';
import 'package:speakup/features/practice/practice.dart';
import 'package:speakup/features/preparation/preparation.dart';
import 'package:speakup/features/review/review.dart';

const _homeRoute = '/';
const _preparationRoute = '/preparation';
const _practiceRoute = '/practice';
const _conversationRoute = '/conversation';
const _reviewRoute = '/review';

class SpeakUpApp extends StatelessWidget {
  const SpeakUpApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'SpeakUp',
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: Colors.indigo),
        useMaterial3: true,
      ),
      initialRoute: _homeRoute,
      routes: {
        _homeRoute: (_) => const _HomePage(),
        _preparationRoute: (_) => const PreparationPage(),
        _practiceRoute: (_) => const PracticePage(),
        _conversationRoute: (_) => const ConversationPage(),
        _reviewRoute: (_) => const ReviewPage(),
      },
    );
  }
}

class _HomePage extends StatelessWidget {
  const _HomePage();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('SpeakUp')),
      body: ListView(
        children: [
          ListTile(
            leading: const Icon(Icons.assignment_outlined),
            title: const Text('Preparation'),
            subtitle: const Text('Prepare the context for a practice session.'),
            trailing: const Icon(Icons.chevron_right),
            onTap: () => Navigator.pushNamed(context, _preparationRoute),
          ),
          ListTile(
            leading: const Icon(Icons.route_outlined),
            title: const Text('Practice'),
            subtitle: const Text('Review the plan for a practice session.'),
            trailing: const Icon(Icons.chevron_right),
            onTap: () => Navigator.pushNamed(context, _practiceRoute),
          ),
          ListTile(
            leading: const Icon(Icons.forum_outlined),
            title: const Text('Conversation'),
            subtitle: const Text('Take part in a guided conversation.'),
            trailing: const Icon(Icons.chevron_right),
            onTap: () => Navigator.pushNamed(context, _conversationRoute),
          ),
          ListTile(
            leading: const Icon(Icons.rate_review_outlined),
            title: const Text('Review'),
            subtitle: const Text('Review feedback after a practice session.'),
            trailing: const Icon(Icons.chevron_right),
            onTap: () => Navigator.pushNamed(context, _reviewRoute),
          ),
        ],
      ),
    );
  }
}
