/// Preparation module boundary.
library;

import 'package:flutter/material.dart';

class PreparationPage extends StatelessWidget {
  const PreparationPage({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('scenes-page'),
      backgroundColor: const Color(0xFFF3F3F0),
      body: SafeArea(
        bottom: false,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 28, 20, 140),
          children: [
            const Text(
              '场景',
              style: TextStyle(fontSize: 32, fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 8),
            const Text(
              '直接进入已经开放的练习；未实现的场景不会提前展示。',
              style: TextStyle(color: Color(0xFF696B73), fontSize: 15),
            ),
            const SizedBox(height: 28),
            Card(
              elevation: 0,
              color: Colors.white,
              clipBehavior: Clip.antiAlias,
              child: Padding(
                padding: const EdgeInsets.all(18),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Container(
                      width: 52,
                      height: 52,
                      decoration: BoxDecoration(
                        color: const Color(0xFFE8E8E5),
                        borderRadius: BorderRadius.circular(16),
                      ),
                      child: const Icon(
                        Icons.work_outline_rounded,
                        color: Color(0xFF4F5054),
                      ),
                    ),
                    const SizedBox(width: 14),
                    const Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            '模拟英文面试',
                            style: TextStyle(
                              fontSize: 18,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                          SizedBox(height: 5),
                          Text(
                            '准备岗位背景、选择面试视角，再开始一场受控练习。',
                            style: TextStyle(
                              color: Color(0xFF696B73),
                              height: 1.4,
                            ),
                          ),
                          SizedBox(height: 10),
                          Text(
                            '页面流程将在后续 Issue 接入',
                            style: TextStyle(
                              color: Color(0xFF4F5054),
                              fontSize: 12,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
