import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index_client.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index_controller.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report_index_decoder.dart';

import 'ielts_speaking_report_fixture.dart';

void main() {
  test('refreshes and appends distinct current report resources', () async {
    final first = decodeIeltsSpeakingReportIndex(
      ieltsSpeakingReportContractFixture()['index_page'],
    );
    final secondItem = _copyItem(
      first.items.single,
      practiceSessionId: 'session_ielts_report_002',
      evaluationId: '7b000103-0000-4000-8000-000000000003',
      evaluationRevisionId: 'a1000103-0000-4000-8000-000000000003',
    );
    final client = _IndexClient([
      first,
      IeltsSpeakingReportIndexPage(items: [secondItem]),
    ]);
    final controller = IeltsSpeakingReportIndexController(client: client);
    addTearDown(controller.dispose);

    await controller.refresh();
    await controller.loadMore();

    expect(controller.items, hasLength(2));
    expect(controller.hasMore, isFalse);
    expect(client.cursors, [null, first.nextCursor]);
  });

  test('rejects a duplicate logical report across pages', () async {
    final first = decodeIeltsSpeakingReportIndex(
      ieltsSpeakingReportContractFixture()['index_page'],
    );
    final client = _IndexClient([
      first,
      IeltsSpeakingReportIndexPage(items: [first.items.single]),
    ]);
    final controller = IeltsSpeakingReportIndexController(client: client);
    addTearDown(controller.dispose);

    await controller.refresh();
    await controller.loadMore();

    expect(controller.items, hasLength(1));
    expect(controller.errorMessage, contains('无法识别'));
  });

  test('account clear fences a late page and clears memory', () async {
    final client = _ControlledIndexClient();
    final controller = IeltsSpeakingReportIndexController(client: client);
    addTearDown(controller.dispose);
    final pending = controller.refresh();
    await client.started.future;

    final clearing = controller.clearPrivateState();
    client.response.complete(
      decodeIeltsSpeakingReportIndex(
        ieltsSpeakingReportContractFixture()['index_page'],
      ),
    );
    await Future.wait([pending, clearing]);

    expect(controller.items, isEmpty);
    expect(controller.errorMessage, isNull);
    expect(client.clearCalls, 1);
  });
}

IeltsSpeakingReportIndexItem _copyItem(
  IeltsSpeakingReportIndexItem source, {
  required String practiceSessionId,
  required String evaluationId,
  required String evaluationRevisionId,
}) => IeltsSpeakingReportIndexItem(
  reportKind: source.reportKind,
  practiceSessionId: practiceSessionId,
  evaluationId: evaluationId,
  evaluationRevisionId: evaluationRevisionId,
  revision: source.revision,
  evaluationStatus: source.evaluationStatus,
  isFinal: source.isFinal,
  statusUrl: '/v1/practice-sessions/$practiceSessionId/ielts-speaking-report',
  createdAt: source.createdAt,
  updatedAt: source.updatedAt,
);

final class _IndexClient implements IeltsSpeakingReportIndexClient {
  _IndexClient(this.pages);

  final List<IeltsSpeakingReportIndexPage> pages;
  final List<String?> cursors = [];

  @override
  Future<IeltsSpeakingReportIndexPage> listReports({
    String? cursor,
    int limit = 20,
  }) async {
    cursors.add(cursor);
    return pages[cursors.length - 1];
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _ControlledIndexClient implements IeltsSpeakingReportIndexClient {
  final started = Completer<void>();
  final response = Completer<IeltsSpeakingReportIndexPage>();
  int clearCalls = 0;

  @override
  Future<IeltsSpeakingReportIndexPage> listReports({
    String? cursor,
    int limit = 20,
  }) {
    started.complete();
    return response.future;
  }

  @override
  Future<void> clearAccountState() async {
    clearCalls++;
  }
}
