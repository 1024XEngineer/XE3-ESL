/// Review module boundary.
library;

import 'package:flutter/material.dart';

class ReviewPage extends StatelessWidget {
  const ReviewPage({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      key: const Key('review-page'),
      backgroundColor: const Color(0xFFF7F8FC),
      body: SafeArea(
        bottom: false,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 28, 20, 140),
          children: const [
            Text(
              '复盘',
              style: TextStyle(fontSize: 32, fontWeight: FontWeight.w800),
            ),
            SizedBox(height: 8),
            Text(
              '练习记录、证据反馈和再次练习会集中在这里。',
              style: TextStyle(color: Color(0xFF696B73), fontSize: 15),
            ),
            SizedBox(height: 28),
            Card(
              elevation: 0,
              color: Colors.white,
              child: Padding(
                padding: EdgeInsets.symmetric(horizontal: 22, vertical: 34),
                child: Column(
                  children: [
                    Icon(
                      Icons.fact_check_outlined,
                      size: 42,
                      color: Color(0xFF8B8E99),
                    ),
                    SizedBox(height: 14),
                    Text(
                      '完成一次练习后再来看看',
                      style: TextStyle(
                        fontSize: 17,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    SizedBox(height: 6),
                    Text(
                      '当前页面为 UI Mock，不包含真实练习数据。',
                      textAlign: TextAlign.center,
                      style: TextStyle(color: Color(0xFF777983)),
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
