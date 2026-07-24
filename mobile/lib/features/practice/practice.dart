/// Practice module boundary.
library;

import 'package:flutter/material.dart';

class PracticePage extends StatelessWidget {
  const PracticePage({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('练习')),
      body: const Center(
        child: Padding(
          padding: EdgeInsets.all(24),
          child: Text(
            '练习模块入口已保留。\n真实 Session 与语音能力将在后续任务接入。',
            textAlign: TextAlign.center,
          ),
        ),
      ),
    );
  }
}
