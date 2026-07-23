/// Preparation module boundary.
library;

import 'package:flutter/material.dart';

class PreparationPage extends StatelessWidget {
  const PreparationPage({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Preparation')),
      body: const Center(child: Text('Preparation module')),
    );
  }
}
