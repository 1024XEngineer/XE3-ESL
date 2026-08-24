import '../support/scene_fixtures.dart';
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/composer/composer_controller.dart';
import 'package:speakup/features/agent/conversation/agent_client.dart';
import 'package:speakup/features/agent/conversation/conversation_controller.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/evaluation_report_detail_page.dart';
import 'package:speakup/features/coaching/review/review.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/features/coaching/practice/practice_client.dart';
import 'package:speakup/features/coaching/practice/practice_controller.dart';
import 'package:speakup/features/coaching/review/evaluation_report_presentation.dart';
import 'package:speakup/features/coaching/review/review_history_client.dart';
import 'package:speakup/features/coaching/review/review_history_controller.dart';
import 'package:speakup/features/coaching/review/review_summary.dart';
import 'package:speakup/features/coaching/review/wire_review_history_client.dart';

import 'evaluation_report_fixture.dart';
import '../support/practice_fixtures.dart';

void main() {
  test('wire client sends Bearer and decodes a bounded stable page', () async {
    const cursor = 'cursor_token';
    final transport = _Transport(
      IdentityHttpResponse(
        statusCode: HttpStatus.ok,
        body: jsonEncode({
          'items': [
            _wireItem(
              id: _newerId,
              createdAt: '2026-07-26T10:00:00Z',
              score: 91,
            ),
            _wireItem(
              id: _olderId,
              createdAt: '2026-07-26T09:00:00Z',
              score: 78,
            ),
          ],
          'next_cursor': cursor,
        }),
      ),
    );
    final client = WireReviewHistoryClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_review-history',
        generation: 4,
      ),
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      transport: transport,
    );

    final page = await client.list(limit: 2);

    expect(page.items.map((item) => item.review.id), [_newerId, _olderId]);
    expect(page.items.first.review.title, '面试复盘');
    expect(
      page.items.first.report.sceneType,
      EvaluationReportSceneType.interview,
    );
    expect(page.items.first.report.dimensions.single.score, 91);
    expect(page.nextCursor, cursor);
    expect(transport.uri.path, '/v1/evaluation-reports');
    expect(transport.uri.queryParameters, {'limit': '2'});
    expect(transport.authorization, 'Bearer sess_review-history');
  });

  test('wire client invalidates the captured Session on 401', () async {
    String? invalidatedToken;
    int? invalidatedGeneration;
    final client = WireReviewHistoryClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_review-history',
        generation: 7,
      ),
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) async {
            invalidatedToken = expectedSessionToken;
            invalidatedGeneration = expectedGeneration;
          },
      transport: _Transport(
        const IdentityHttpResponse(
          statusCode: HttpStatus.unauthorized,
          body: '{}',
        ),
      ),
    );

    await expectLater(
      client.list(),
      throwsA(
        isA<ReviewHistoryException>().having(
          (error) => error.kind,
          'kind',
          ReviewHistoryFailureKind.authenticationRequired,
        ),
      ),
    );
    expect(invalidatedToken, 'sess_review-history');
    expect(invalidatedGeneration, 7);
  });

  test(
    'default transport rejects an oversized Content-Length before decoding',
    () async {
      await _withRealHttp(() async {
        final server = await _startReviewHistoryServer((request) async {
          request.response.contentLength = _testMaximumHistoryResponseBytes + 1;
          request.response.add(const <int>[0x7B]);
          await request.response.flush();
          await request.response.close();
        });
        addTearDown(() => server.close(force: true));
        final client = _defaultTransportClient(server);

        await expectLater(client.list(), throwsA(_invalidHistoryResponse));
      });
    },
  );

  test('default transport rejects a chunked response over 1 MiB', () async {
    await _withRealHttp(() async {
      final server = await _startReviewHistoryServer((request) async {
        request.response.headers.chunkedTransferEncoding = true;
        request.response.bufferOutput = false;
        final chunk = List<int>.filled(64 * 1024, 0x20);
        for (var index = 0; index < 17; index++) {
          request.response.add(chunk);
          await request.response.flush();
        }
        await request.response.close();
      });
      addTearDown(() => server.close(force: true));
      final client = _defaultTransportClient(server);

      await expectLater(client.list(), throwsA(_invalidHistoryResponse));
    });
  });

  test('default transport rejects malformed UTF-8', () async {
    await _withRealHttp(() async {
      final server = await _startReviewHistoryServer((request) async {
        request.response.contentLength = 2;
        request.response.add(const <int>[0xC3, 0x28]);
        await request.response.close();
      });
      addTearDown(() => server.close(force: true));
      final client = _defaultTransportClient(server);

      await expectLater(client.list(), throwsA(_invalidHistoryResponse));
    });
  });

  test('account cleanup discards a late history response', () async {
    final client = _ControlledClient();
    final controller = ReviewHistoryController(client: client);
    addTearDown(controller.dispose);

    final refresh = controller.refresh();
    await Future<void>.delayed(Duration.zero);
    final cleanup = controller.clearPrivateState();
    client.complete(ReviewHistoryPage(items: [_item(_newerId, score: 91)]));
    await Future.wait([refresh, cleanup]);

    expect(controller.items, isEmpty);
    expect(controller.errorMessage, isNull);
  });

  test(
    'concurrent refreshes serialize and coalesce one pending refresh',
    () async {
      final client = _SequencedControlledClient();
      final controller = ReviewHistoryController(client: client);
      addTearDown(controller.dispose);

      final first = controller.refresh();
      final second = controller.refresh();
      final third = controller.refresh();

      expect(client.requests, hasLength(1));
      expect(client.requests.single.cursor, isNull);

      client.complete(
        0,
        ReviewHistoryPage(items: [_item(_olderId, score: 78)]),
      );
      await first;

      expect(client.requests, hasLength(2));
      expect(client.requests.last.cursor, isNull);

      client.complete(
        1,
        ReviewHistoryPage(items: [_item(_newerId, score: 91)]),
      );
      await Future.wait([second, third]);

      expect(client.requests, hasLength(2));
      expect(controller.items.single.review.id, _newerId);
      expect(controller.errorMessage, isNull);
    },
  );

  test(
    'cleanup removes a queued refresh before it can call the client',
    () async {
      final client = _SequencedControlledClient();
      final controller = ReviewHistoryController(client: client);
      addTearDown(controller.dispose);

      final active = controller.refresh();
      final queued = controller.refresh();
      expect(client.requests, hasLength(1));

      await controller.clearPrivateState();
      await Future.wait([active, queued]);

      expect(client.clearCount, 1);
      expect(client.requests, hasLength(1));
      expect(controller.isLoading, isFalse);

      client.complete(
        0,
        ReviewHistoryPage(items: [_item(_newerId, score: 91)]),
      );
      await Future<void>.delayed(Duration.zero);

      expect(client.requests, hasLength(1));
      expect(controller.items, isEmpty);
      expect(controller.errorMessage, isNull);
    },
  );

  test('load-more double tap is single-flight for the same cursor', () async {
    final client = _SequencedControlledClient();
    final controller = ReviewHistoryController(client: client);
    addTearDown(controller.dispose);

    final initial = controller.refresh();
    client.complete(
      0,
      ReviewHistoryPage(
        items: [_item(_newerId, score: 91)],
        nextCursor: 'older_cursor',
      ),
    );
    await initial;

    final first = controller.loadMore();
    final second = controller.loadMore();

    expect(client.requests, hasLength(2));
    expect(client.requests.last.cursor, 'older_cursor');

    client.complete(1, ReviewHistoryPage(items: [_item(_olderId, score: 78)]));
    await Future.wait([first, second]);

    expect(client.requests, hasLength(2));
    expect(controller.items.map((item) => item.review.id), [
      _newerId,
      _olderId,
    ]);
  });

  test(
    'new account refresh starts immediately and ignores the detached old error',
    () async {
      final client = _SequencedControlledClient();
      final controller = ReviewHistoryController(client: client);
      addTearDown(controller.dispose);

      final oldAccount = controller.refresh();
      expect(client.requests, hasLength(1));

      await controller.clearPrivateState();
      await oldAccount;
      final newAccount = controller.refresh();

      expect(client.requests, hasLength(2));
      client.complete(
        1,
        ReviewHistoryPage(items: [_item(_newerId, score: 91)]),
      );
      await newAccount;

      expect(controller.items.single.review.id, _newerId);
      expect(controller.errorMessage, isNull);

      client.fail(
        0,
        const ReviewHistoryException(
          kind: ReviewHistoryFailureKind.network,
          retryable: true,
        ),
      );
      await Future<void>.delayed(Duration.zero);

      expect(controller.items.single.review.id, _newerId);
      expect(controller.errorMessage, isNull);
      expect(controller.isLoading, isFalse);
    },
  );

  testWidgets('Shell refreshes history only when the Review tab is opened', (
    tester,
  ) async {
    final client = _SequencedControlledClient();
    final historyController = ReviewHistoryController(client: client);
    final harness = await _completedShellHarness(_newerId);
    addTearDown(historyController.dispose);
    addTearDown(harness.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: SpeakUpShell(
          conversationController: harness.conversation,
          composerController: harness.composer,
          practiceController: harness.practice,
          reviewHistoryController: historyController,
        ),
      ),
    );
    await tester.pump();

    expect(client.requests, isEmpty);
    expect(find.byKey(const Key('agent-home-page')), findsOneWidget);

    await tester.tap(find.byKey(const Key('primary-tab-review')));
    await tester.pump();

    expect(client.requests, hasLength(1));
    expect(client.requests.single.cursor, isNull);

    client.complete(0, ReviewHistoryPage(items: [_item(_olderId, score: 78)]));
    await tester.pumpAndSettle();
    await _expandHistory(tester);

    expect(find.byKey(const Key('review-history-$_olderId')), findsOneWidget);
    expect(client.requests, hasLength(1));
  });

  testWidgets('Shell refreshes cached history when the Profile tab is opened', (
    tester,
  ) async {
    final client = _SequencedControlledClient();
    final historyController = ReviewHistoryController(client: client);
    final harness = await _completedShellHarness(_newerId);
    addTearDown(historyController.dispose);
    addTearDown(harness.dispose);

    final initialRefresh = historyController.refresh();
    client.complete(0, ReviewHistoryPage(items: [_item(_olderId, score: 78)]));
    await initialRefresh;

    await tester.pumpWidget(
      MaterialApp(
        home: SpeakUpShell(
          conversationController: harness.conversation,
          composerController: harness.composer,
          practiceController: harness.practice,
          reviewHistoryController: historyController,
        ),
      ),
    );
    await tester.pump();

    expect(historyController.items, hasLength(1));
    expect(client.requests, hasLength(1));

    await tester.tap(find.byKey(const Key('primary-tab-profile')));
    await tester.pump();

    expect(client.requests, hasLength(2));
    expect(client.requests.last.cursor, isNull);
  });

  testWidgets('Practice restore does not trigger Review history loading', (
    tester,
  ) async {
    final client = _SequencedControlledClient();
    final historyController = ReviewHistoryController(client: client);
    final harness = _configuredCompletedShellHarness(_newerId);
    addTearDown(historyController.dispose);
    addTearDown(harness.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: SpeakUpShell(
          conversationController: harness.conversation,
          composerController: harness.composer,
          practiceController: harness.practice,
          reviewHistoryController: historyController,
        ),
      ),
    );
    await tester.pump();

    expect(client.requests, isEmpty);
    expect(find.byKey(const Key('agent-home-page')), findsOneWidget);

    await _restoreCompletedPractice(harness, _newerId);
    await tester.pump();

    expect(client.requests, isEmpty);
    expect(find.byKey(const Key('agent-home-page')), findsOneWidget);

    await tester.tap(find.byKey(const Key('primary-tab-review')));
    await tester.pump();

    expect(client.requests, hasLength(1));
    expect(client.requests.single.cursor, isNull);

    client.complete(0, const ReviewHistoryPage(items: []));
    await tester.pumpAndSettle();
    await _expandHistory(tester);

    expect(client.requests, hasLength(1));
    expect(find.byKey(const Key('review-availability-title')), findsOneWidget);
  });

  testWidgets('Shell rebuilds do not duplicate a Review tab refresh', (
    tester,
  ) async {
    final firstClient = _SequencedControlledClient();
    final firstHistoryController = ReviewHistoryController(client: firstClient);
    final firstHarness = await _completedShellHarness(_newerId);
    final secondHarness = await _completedShellHarness(_olderId);
    addTearDown(firstHistoryController.dispose);
    addTearDown(firstHarness.dispose);
    addTearDown(secondHarness.dispose);

    var harness = firstHarness;
    late StateSetter rebuild;
    await tester.pumpWidget(
      MaterialApp(
        home: StatefulBuilder(
          builder: (context, setState) {
            rebuild = setState;
            return SpeakUpShell(
              conversationController: harness.conversation,
              composerController: harness.composer,
              practiceController: harness.practice,
              reviewHistoryController: firstHistoryController,
            );
          },
        ),
      ),
    );
    await tester.pump();

    expect(firstClient.requests, isEmpty);

    for (var index = 0; index < 5; index++) {
      rebuild(() {});
      await tester.pump();
    }
    expect(firstClient.requests, isEmpty);

    await tester.tap(find.byKey(const Key('primary-tab-review')));
    await tester.pump();
    expect(firstClient.requests, hasLength(1));

    rebuild(() => harness = secondHarness);
    await tester.pump();
    for (var index = 0; index < 5; index++) {
      rebuild(() {});
      await tester.pump();
    }

    expect(firstClient.requests, hasLength(1));
    firstClient.complete(0, const ReviewHistoryPage(items: []));
    for (var index = 0; index < 5; index++) {
      rebuild(() {});
      await tester.pump();
    }

    expect(firstClient.requests, hasLength(1));
  });

  testWidgets(
    'Review tab shows server history, selection, pagination, and retry states',
    (tester) async {
      final client = _PagedClient();
      final controller = ReviewHistoryController(client: client);
      addTearDown(controller.dispose);

      await tester.pumpWidget(
        MaterialApp(home: ReviewPage(historyController: controller)),
      );
      await tester.pump();
      expect(
        find.byKey(const Key('review-history-initial-loading')),
        findsOneWidget,
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.byKey(const Key('review-page')), findsOneWidget);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('review-content')), findsOneWidget);
      expect(find.byKey(const Key('review-title')), findsOneWidget);
      await _ensureHistoryVisible(
        tester,
        find.byKey(const Key('review-history-load-more')),
      );
      expect(find.byKey(const Key('review-history-load-more')), findsOneWidget);

      await _ensureHistoryVisible(
        tester,
        find.byKey(const Key('review-history-select-$_newerId')),
      );
      await tester.tap(
        find.byKey(const Key('review-history-select-$_newerId')),
      );
      await tester.pumpAndSettle();
      expect(
        find.byKey(const Key('evaluation-report-detail-page')),
        findsOneWidget,
      );
      expect(find.text('focus-91'), findsWidgets);
      expect(find.text('focus-78'), findsNothing);

      await tester.tap(find.byKey(const Key('evaluation-report-detail-back')));
      await tester.pumpAndSettle();
      await _ensureHistoryVisible(
        tester,
        find.byKey(const Key('review-history-select-$_olderId')),
      );
      await tester.tap(
        find.byKey(const Key('review-history-select-$_olderId')),
      );
      await tester.pumpAndSettle();
      expect(
        find.byKey(const Key('evaluation-report-detail-page')),
        findsOneWidget,
      );
      expect(find.text('focus-78'), findsWidgets);
      expect(find.text('focus-91'), findsNothing);

      await tester.tap(find.byKey(const Key('evaluation-report-detail-back')));
      await tester.pumpAndSettle();
      await _ensureHistoryVisible(
        tester,
        find.byKey(const Key('review-history-load-more')),
      );
      await tester.tap(find.byKey(const Key('review-history-load-more')));
      await tester.pumpAndSettle();
      expect(
        find.byKey(const Key('review-history-$_oldestId')),
        findsOneWidget,
      );
      expect(find.byKey(const Key('review-history-load-more')), findsNothing);
    },
  );

  testWidgets('Review tab renders zero-item and retryable failure states', (
    tester,
  ) async {
    final failureClient = _FailOnceClient();
    final failureController = ReviewHistoryController(client: failureClient);
    addTearDown(failureController.dispose);
    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: failureController)),
    );
    await tester.pump();
    expect(
      find.byKey(const Key('review-history-initial-loading')),
      findsOneWidget,
    );
    await _expandHistory(tester);
    expect(find.byKey(const Key('review-history-error')), findsOneWidget);

    await _ensureHistoryVisible(
      tester,
      find.byKey(const Key('review-history-retry')),
    );
    await tester.tap(find.byKey(const Key('review-history-retry')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('review-content')), findsOneWidget);

    final emptyController = ReviewHistoryController(client: _EmptyClient());
    addTearDown(emptyController.dispose);
    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: emptyController)),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('review-availability-title')), findsOneWidget);
    expect(find.byKey(const Key('review-content')), findsNothing);
    expect(find.textContaining('三轮'), findsNothing);
  });

  testWidgets(
    'refresh failure with an existing next page retries without a cursor',
    (tester) async {
      final client = _RefreshRetryClient();
      final controller = ReviewHistoryController(client: client);
      addTearDown(controller.dispose);

      await tester.pumpWidget(
        MaterialApp(home: ReviewPage(historyController: controller)),
      );
      await tester.pumpAndSettle();
      await _expandHistory(tester);

      expect(client.cursors, <String?>[null]);
      expect(controller.hasMore, isTrue);

      final failedRefresh = controller.refresh();
      await tester.pumpAndSettle();
      await failedRefresh;

      expect(client.cursors, <String?>[null, null]);
      expect(
        find.byKey(const Key('review-history-page-error')),
        findsOneWidget,
      );

      await _ensureHistoryVisible(
        tester,
        find.byKey(const Key('review-history-page-retry')),
      );
      await tester.tap(find.byKey(const Key('review-history-page-retry')));
      await tester.pumpAndSettle();

      expect(client.cursors, <String?>[null, null, null]);
      expect(find.byKey(const Key('review-history-$_newerId')), findsOneWidget);
      expect(controller.hasMore, isFalse);
    },
  );

  testWidgets('load-more failure retries the original cursor', (tester) async {
    final client = _LoadMoreRetryClient();
    final controller = ReviewHistoryController(client: client);
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: controller)),
    );
    await tester.pumpAndSettle();
    await _expandHistory(tester);

    await _ensureHistoryVisible(
      tester,
      find.byKey(const Key('review-history-load-more')),
    );
    await tester.tap(find.byKey(const Key('review-history-load-more')));
    await tester.pumpAndSettle();

    expect(client.cursors, <String?>[null, 'older_cursor']);
    expect(find.byKey(const Key('review-history-page-error')), findsOneWidget);

    await _ensureHistoryVisible(
      tester,
      find.byKey(const Key('review-history-page-retry')),
    );
    await tester.tap(find.byKey(const Key('review-history-page-retry')));
    await tester.pumpAndSettle();

    expect(client.cursors, <String?>[null, 'older_cursor', 'older_cursor']);
    expect(find.byKey(const Key('review-history-$_olderId')), findsOneWidget);
    expect(controller.hasMore, isFalse);
  });

  testWidgets('server Review history appears after its initial load', (
    tester,
  ) async {
    final historyClient = _ControlledClient();
    final historyController = ReviewHistoryController(client: historyClient);
    addTearDown(historyController.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: historyController)),
    );
    await tester.pump();

    expect(find.byKey(const Key('review-content')), findsNothing);
    expect(
      find.byKey(const Key('review-history-initial-loading')),
      findsOneWidget,
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    expect(
      find.byKey(const Key('review-history-initial-loading')),
      findsOneWidget,
    );

    historyClient.complete(
      ReviewHistoryPage(items: [_item(_newerId, score: 91)]),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('review-current-$_newerId')), findsNothing);
    expect(find.byKey(const Key('review-history-$_newerId')), findsOneWidget);
    expect(find.byKey(const Key('review-content')), findsOneWidget);
    expect(find.byKey(const Key('review-title')), findsOneWidget);
  });

  testWidgets('history failure and empty history expose their own states', (
    tester,
  ) async {
    final failureController = ReviewHistoryController(
      client: _AlwaysFailClient(),
    );
    addTearDown(failureController.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: failureController)),
    );
    await tester.pumpAndSettle();
    await _expandHistory(tester);

    expect(find.byKey(const Key('review-content')), findsNothing);
    expect(find.byKey(const Key('review-history-error')), findsOneWidget);

    final emptyController = ReviewHistoryController(client: _EmptyClient());
    addTearDown(emptyController.dispose);
    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: emptyController)),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('review-content')), findsNothing);
    expect(find.byKey(const Key('review-availability-title')), findsOneWidget);
  });

  testWidgets('multiple server Reviews remain distinct selectable results', (
    tester,
  ) async {
    final historyController = ReviewHistoryController(
      client: _FixedItemsClient(<ReviewHistoryItem>[
        _item(_newerId, score: 91),
        _item(_olderId, score: 78),
      ]),
    );
    addTearDown(historyController.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: historyController)),
    );
    await tester.pumpAndSettle();
    await _expandHistory(tester);

    expect(find.byKey(const Key('review-history-$_newerId')), findsOneWidget);
    expect(find.byKey(const Key('review-history-$_olderId')), findsOneWidget);
    expect(find.byKey(const Key('review-content')), findsOneWidget);
    expect(find.text('summary-91'), findsNothing);
    expect(find.text('summary-78'), findsNothing);

    await _ensureHistoryVisible(
      tester,
      find.byKey(const Key('review-history-select-$_newerId')),
    );
    await tester.tap(find.byKey(const Key('review-history-select-$_newerId')));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('evaluation-report-detail-page')),
      findsOneWidget,
    );
    expect(find.text('focus-91'), findsWidgets);
    expect(find.text('focus-78'), findsNothing);

    await tester.tap(find.byKey(const Key('evaluation-report-detail-back')));
    await tester.pumpAndSettle();
    await _ensureHistoryVisible(
      tester,
      find.byKey(const Key('review-history-select-$_olderId')),
    );
    await tester.tap(find.byKey(const Key('review-history-select-$_olderId')));
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('evaluation-report-detail-page')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('evaluation-report-detail-title')),
      findsOneWidget,
    );
    expect(find.text('focus-91'), findsNothing);
    expect(find.text('focus-78'), findsWidgets);
  });

  testWidgets('one history item opens a dedicated detail page', (tester) async {
    final item = _fixtureItem(index: 0);
    final controller = ReviewHistoryController(
      client: _FixedItemsClient(<ReviewHistoryItem>[item]),
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: controller)),
    );
    await tester.pumpAndSettle();
    await _expandHistory(tester);

    expect(find.byKey(Key('review-history-${item.review.id}')), findsOneWidget);
    expect(
      find.byKey(const Key('evaluation-report-detail-page')),
      findsNothing,
    );
    expect(find.text(item.review.summary), findsNothing);
    expect(
      tester
          .getSize(find.byKey(Key('review-history-${item.review.id}')))
          .height,
      lessThan(145),
    );

    await tester.tap(
      find.byKey(Key('review-history-select-${item.review.id}')),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('evaluation-report-detail-page')),
      findsOneWidget,
    );
    expect(find.byKey(const Key('evaluation-report-overview')), findsOneWidget);
    expect(
      find.byKey(const Key('evaluation-report-dimensions')),
      findsOneWidget,
    );
    expect(find.text(item.review.summary), findsNothing);

    await tester.tap(find.byKey(const Key('evaluation-report-detail-back')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('review-history-list')), findsOneWidget);
    expect(
      find.byKey(const Key('evaluation-report-detail-page')),
      findsNothing,
    );

    await tester.tap(
      find.byKey(Key('review-history-select-${item.review.id}')),
    );
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('evaluation-report-detail-page')),
      findsOneWidget,
    );
  });

  testWidgets('canonical report renders dimensions and priority improvements', (
    tester,
  ) async {
    final item = _sceneItem(
      id: 'review-v2-interview',
      sceneType: EvaluationReportSceneType.interview,
      scoreability: EvaluationReportScoreability.provisional,
      dimensions: const <EvaluationReportDimension>[
        EvaluationReportDimension(
          key: 'INTERVIEW_STRUCTURE',
          score: 82,
          scale: EvaluationReportScoreScale.percentage100,
          coverage: 1,
          confidence: 0.8,
          reasonCodes: <String>['ASR_CONFIDENCE_UNAVAILABLE'],
          evidenceRefIds: <String>['evidence_1'],
          strengths: <EvaluationReportFinding>[
            EvaluationReportFinding(
              id: 'strength_1',
              message: '回答紧扣问题，并按背景、行动、结果展开。',
              evidence: <EvaluationReportEvidence>[],
            ),
          ],
          improvements: <EvaluationReportFinding>[
            EvaluationReportFinding(
              id: 'correction_1',
              message: 'I responsible for the migration.',
              suggestion: 'I was responsible for the migration.',
              evidence: <EvaluationReportEvidence>[],
            ),
          ],
          recommendedExamples: <EvaluationReportFinding>[],
        ),
        EvaluationReportDimension(
          key: 'INTERVIEW_EVIDENCE',
          score: 76,
          scale: EvaluationReportScoreScale.percentage100,
          coverage: 1,
          confidence: 0.8,
          reasonCodes: <String>['ASR_CONFIDENCE_UNAVAILABLE'],
          evidenceRefIds: <String>['evidence_2'],
          strengths: <EvaluationReportFinding>[],
          improvements: <EvaluationReportFinding>[
            EvaluationReportFinding(
              id: 'correction_2',
              message: 'Add one measurable outcome.',
              suggestion: 'Name the time saved after the migration.',
              evidence: <EvaluationReportEvidence>[],
            ),
          ],
          recommendedExamples: <EvaluationReportFinding>[],
        ),
      ],
      priorityActions: const <EvaluationReportPriorityAction>[
        EvaluationReportPriorityAction(
          dimensionKey: 'INTERVIEW_STRUCTURE',
          findingId: 'correction_1',
        ),
      ],
    );
    final controller = ReviewHistoryController(
      client: _FixedItemsClient(<ReviewHistoryItem>[item]),
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: controller)),
    );
    await tester.pumpAndSettle();
    await _expandHistory(tester);
    await tester.tap(
      find.byKey(Key('review-history-select-${item.review.id}')),
    );
    await tester.pumpAndSettle();

    expect(find.text('面试复盘'), findsOneWidget);
    expect(
      find.byKey(const Key('evaluation-report-dimensions')),
      findsOneWidget,
    );
    expect(find.text('回答结构'), findsOneWidget);
    expect(find.text('82 / 100'), findsOneWidget);
    expect(find.text('优先项在前'), findsOneWidget);
    expect(find.text('I responsible for the migration.'), findsOneWidget);
    expect(find.text('I was responsible for the migration.'), findsOneWidget);
    expect(find.text('Add one measurable outcome.'), findsOneWidget);
    expect(
      find.text('Name the time saved after the migration.'),
      findsOneWidget,
    );
    expect(find.textContaining('面试复盘 ·'), findsNothing);
  });

  testWidgets('canonical IELTS report uses the IELTS score scale', (
    tester,
  ) async {
    final item = _sceneItem(
      id: 'review-v2-ielts',
      sceneType: EvaluationReportSceneType.ieltsSpeaking,
      scoreability: EvaluationReportScoreability.provisional,
      dimensions: const <EvaluationReportDimension>[
        EvaluationReportDimension(
          key: 'FLUENCY_COHERENCE',
          score: 7.5,
          scale: EvaluationReportScoreScale.ieltsBand,
          coverage: 1,
          confidence: 0.8,
          reasonCodes: <String>['ASR_CONFIDENCE_UNAVAILABLE'],
          evidenceRefIds: <String>['evidence_1'],
          strengths: <EvaluationReportFinding>[],
          improvements: <EvaluationReportFinding>[],
          recommendedExamples: <EvaluationReportFinding>[],
        ),
        EvaluationReportDimension(
          key: 'LEXICAL_RESOURCE',
          score: 6,
          scale: EvaluationReportScoreScale.ieltsBand,
          coverage: 1,
          confidence: 0.8,
          reasonCodes: <String>[],
          evidenceRefIds: <String>[],
          strengths: <EvaluationReportFinding>[],
          improvements: <EvaluationReportFinding>[],
          recommendedExamples: <EvaluationReportFinding>[],
        ),
        EvaluationReportDimension(
          key: 'GRAMMATICAL_RANGE_ACCURACY',
          score: 6.5,
          scale: EvaluationReportScoreScale.ieltsBand,
          coverage: 1,
          confidence: 0.8,
          reasonCodes: <String>[],
          evidenceRefIds: <String>[],
          strengths: <EvaluationReportFinding>[],
          improvements: <EvaluationReportFinding>[],
          recommendedExamples: <EvaluationReportFinding>[],
        ),
        EvaluationReportDimension(
          key: 'PRONUNCIATION',
          score: 4.5,
          scale: EvaluationReportScoreScale.ieltsBand,
          coverage: 0.87,
          confidence: 0.8,
          reasonCodes: <String>['PARTIAL_ACOUSTIC_COVERAGE'],
          evidenceRefIds: <String>[],
          strengths: <EvaluationReportFinding>[],
          improvements: <EvaluationReportFinding>[],
          recommendedExamples: <EvaluationReportFinding>[],
        ),
      ],
    );
    final controller = ReviewHistoryController(
      client: _FixedItemsClient(<ReviewHistoryItem>[item]),
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: controller)),
    );
    await tester.pumpAndSettle();
    await _expandHistory(tester);
    expect(find.text('IELTS 模考'), findsOneWidget);
    await tester.tap(
      find.byKey(Key('review-history-select-${item.review.id}')),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('evaluation-report-insufficient-notice')),
      findsNothing,
    );
    expect(find.text('题目与作答（1）'), findsOneWidget);
    expect(find.text('7.5 / 9'), findsOneWidget);
    expect(find.byKey(const Key('evaluation-report-radar')), findsOneWidget);
    expect(find.text('分项详情与建议'), findsOneWidget);
    for (final key in const <String>[
      'FLUENCY_COHERENCE',
      'LEXICAL_RESOURCE',
      'GRAMMATICAL_RANGE_ACCURACY',
      'PRONUNCIATION',
    ]) {
      expect(
        find.byKey(Key('evaluation-report-dimension-$key')),
        findsOneWidget,
      );
    }
    expect(
      tester
          .widget<Text>(
            find.byKey(const Key('evaluation-report-overall-score')),
          )
          .data,
      '6',
    );
    expect(find.text('证据覆盖 87%'), findsOneWidget);
  });

  testWidgets(
    'Part 1 report shows score and priority actions before collapsed details',
    (tester) async {
      const grammarFinding = EvaluationReportFinding(
        id: 'grammar-priority',
        message: '第三人称单数和句子结构需要更准确。',
        suggestion: '使用 it gives，并把断开的句子合并完整。',
        evidence: <EvaluationReportEvidence>[
          EvaluationReportEvidence(
            evidenceRefId: 'grammar-evidence',
            turnId: 'turn-grammar',
            startUtf8Byte: 0,
            endUtf8Byte: 12,
            originalExcerpt: 'it give me energy',
          ),
        ],
      );
      const secondaryGrammarFinding = EvaluationReportFinding(
        id: 'grammar-secondary',
        message: '这条普通语法建议不应盖过后端指定的优先项。',
        suggestion: '先处理优先项后再看这条建议。',
        evidence: <EvaluationReportEvidence>[],
      );
      const pronunciationFinding = EvaluationReportFinding(
        id: 'pronunciation-priority',
        message: '部分音素影响了表达清晰度。',
        suggestion: '先慢速重读目标词，再恢复正常语速。',
        evidence: <EvaluationReportEvidence>[
          EvaluationReportEvidence(
            evidenceRefId: 'pronunciation-evidence',
            turnId: 'turn-pronunciation',
            startUtf8Byte: 0,
            endUtf8Byte: 10,
            originalExcerpt: 'music theory',
          ),
        ],
      );
      final item = _sceneItem(
        id: 'review-v2-ielts-part1',
        sceneType: EvaluationReportSceneType.ieltsSpeaking,
        scoreability: EvaluationReportScoreability.provisional,
        practiceMode: 'PART_1',
        summary: '这段较长的总结不应抢在估分前展示。',
        dimensions: <EvaluationReportDimension>[
          _part1Dimension(
            key: 'FLUENCY_COHERENCE',
            score: 7.5,
            improvementMessage: '完整流利性详情',
          ),
          _part1Dimension(
            key: 'LEXICAL_RESOURCE',
            score: 7,
            improvementMessage: '完整词汇详情',
          ),
          _part1Dimension(
            key: 'GRAMMATICAL_RANGE_ACCURACY',
            score: 6.5,
            improvements: const <EvaluationReportFinding>[
              secondaryGrammarFinding,
              grammarFinding,
            ],
          ),
          _part1Dimension(
            key: 'PRONUNCIATION',
            score: 6,
            improvement: pronunciationFinding,
          ),
        ],
        priorityActions: const <EvaluationReportPriorityAction>[
          EvaluationReportPriorityAction(
            dimensionKey: 'GRAMMATICAL_RANGE_ACCURACY',
            findingId: 'grammar-priority',
          ),
          EvaluationReportPriorityAction(
            dimensionKey: 'PRONUNCIATION',
            findingId: 'pronunciation-priority',
          ),
        ],
      );
      final controller = ReviewHistoryController(
        client: _FixedItemsClient(<ReviewHistoryItem>[item]),
      );
      addTearDown(controller.dispose);

      await tester.pumpWidget(
        MaterialApp(home: ReviewPage(historyController: controller)),
      );
      await tester.pumpAndSettle();
      await _expandHistory(tester);
      await tester.tap(
        find.byKey(Key('review-history-select-${item.review.id}')),
      );
      await tester.pumpAndSettle();

      final darkOverview = find.byKey(const Key('evaluation-report-overview'));
      expect(darkOverview, findsOneWidget);
      expect(find.text('基于本次 1 道已记录回答的阶段性估分，不等同于官方考试成绩。'), findsOneWidget);
      expect(find.byKey(const Key('evaluation-report-radar')), findsOneWidget);
      // Summary is not shown for Part 1 reports (score speaks first).
      expect(find.text('这段较长的总结不应抢在估分前展示。'), findsNothing);
      expect(find.text('优先项在前'), findsOneWidget);

      // The server-selected grammar action stays ahead of the lower-scored
      // pronunciation card, and its exact finding is the visible first item.
      final grammar = find.byKey(
        const Key('evaluation-report-dimension-GRAMMATICAL_RANGE_ACCURACY'),
      );
      final pronunciation = find.byKey(
        const Key('evaluation-report-dimension-PRONUNCIATION'),
      );
      await tester.ensureVisible(pronunciation);
      await tester.pumpAndSettle();
      expect(grammar, findsOneWidget);
      expect(pronunciation, findsOneWidget);
      expect(
        tester.getTopLeft(grammar).dy,
        lessThan(tester.getTopLeft(pronunciation).dy),
      );
      expect(find.text('第三人称单数和句子结构需要更准确。'), findsOneWidget);
      expect(find.text('使用 it gives，并把断开的句子合并完整。'), findsOneWidget);
      expect(find.text('这条普通语法建议不应盖过后端指定的优先项。'), findsNothing);

      // Questions disclosure exists and works.
      final questions = find.byKey(
        const Key('evaluation-report-questions-disclosure'),
      );
      await tester.ensureVisible(questions);
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const Key('evaluation-report-questions-toggle')),
      );
      await tester.pumpAndSettle();
      await tester.ensureVisible(
        find.text('1. Tell me about your experience.', skipOffstage: false),
      );
      await tester.pumpAndSettle();
      expect(find.text('1. Tell me about your experience.'), findsOneWidget);
    },
  );

  testWidgets(
    'Part 1 report renders dimensions with missing findings gracefully',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(800, 2000));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final item = _sceneItem(
        id: 'review-v2-ielts-part1-no-action',
        sceneType: EvaluationReportSceneType.ieltsSpeaking,
        scoreability: EvaluationReportScoreability.provisional,
        practiceMode: 'PART_1',
        dimensions: <EvaluationReportDimension>[
          _part1Dimension(key: 'FLUENCY_COHERENCE', score: 7),
        ],
      );
      final controller = ReviewHistoryController(
        client: _FixedItemsClient(<ReviewHistoryItem>[item]),
      );
      addTearDown(controller.dispose);

      await tester.pumpWidget(
        MaterialApp(home: ReviewPage(historyController: controller)),
      );
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(Key('review-history-select-${item.review.id}')),
      );
      await tester.pumpAndSettle();

      final scored = find.byKey(
        const Key('evaluation-report-dimension-FLUENCY_COHERENCE'),
      );
      final unscored = find.byKey(
        const Key('evaluation-report-dimension-LEXICAL_RESOURCE'),
      );
      expect(find.text('从薄弱项开始'), findsOneWidget);
      expect(
        tester.getTopLeft(scored).dy,
        lessThan(tester.getTopLeft(unscored).dy),
      );
      expect(find.text('7 / 9'), findsWidgets);
    },
  );

  testWidgets('profile shows IELTS ability instead of the report narrative', (
    tester,
  ) async {
    final item = _sceneItem(
      id: 'profile-ielts-ability',
      sceneType: EvaluationReportSceneType.ieltsSpeaking,
      scoreability: EvaluationReportScoreability.provisional,
      dimensions: [
        for (final entry in const <String, double>{
          'FLUENCY_COHERENCE': 4,
          'PRONUNCIATION': 4.5,
          'GRAMMATICAL_RANGE_ACCURACY': 3,
          'LEXICAL_RESOURCE': 3.5,
        }.entries)
          EvaluationReportDimension(
            key: entry.key,
            score: entry.value,
            scale: EvaluationReportScoreScale.ieltsBand,
            coverage: 1,
            confidence: 0.8,
            reasonCodes: const <String>[],
            evidenceRefIds: const <String>[],
            strengths: const <EvaluationReportFinding>[],
            improvements: const <EvaluationReportFinding>[],
            recommendedExamples: const <EvaluationReportFinding>[],
          ),
      ],
    );
    final controller = ReviewHistoryController(
      client: _FixedItemsClient(<ReviewHistoryItem>[item]),
    );
    addTearDown(controller.dispose);
    await controller.refresh();

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CurrentIeltsAbilityProfile(historyController: controller),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('当前 IELTS 能力'), findsOneWidget);
    expect(find.byKey(const Key('evaluation-profile-radar')), findsOneWidget);
    expect(find.text('本次回答已经形成可复盘的文本反馈。'), findsNothing);
    expect(
      tester
          .widget<Text>(
            find.byKey(const Key('evaluation-profile-overall-score')),
          )
          .data,
      '4',
    );
  });

  testWidgets('generic four-dimension report renders each dimension', (
    tester,
  ) async {
    final dimensions = <EvaluationReportDimension>[
      for (final entry in const <String, double>{
        'TASK_ACHIEVEMENT': 5,
        'CLARITY_COHERENCE': 10,
        'LANGUAGE_CONTROL': 15,
        'INTERACTION': 5,
      }.entries)
        EvaluationReportDimension(
          key: entry.key,
          score: entry.value,
          scale: EvaluationReportScoreScale.percentage100,
          coverage: 1,
          confidence: 0.8,
          reasonCodes: const <String>[],
          evidenceRefIds: const <String>[],
          strengths: entry.key == 'TASK_ACHIEVEMENT'
              ? const <EvaluationReportFinding>[
                  EvaluationReportFinding(
                    id: 'strength-task',
                    message: 'The response advances the communication goal.',
                    evidence: <EvaluationReportEvidence>[],
                  ),
                ]
              : const <EvaluationReportFinding>[],
          improvements: const <EvaluationReportFinding>[],
          recommendedExamples: const <EvaluationReportFinding>[],
        ),
    ];
    final item = _sceneItem(
      id: 'review-v2-workplace-radar',
      sceneType: EvaluationReportSceneType.overseasWorkplace,
      scoreability: EvaluationReportScoreability.provisional,
      dimensions: dimensions,
    );

    await tester.pumpWidget(
      MaterialApp(home: ReviewReportDetailPage(item: item)),
    );

    expect(find.byKey(const Key('review-detail-summary')), findsNothing);
    expect(find.textContaining('/ 100'), findsNWidgets(5));
    expect(
      find.text('The response advances the communication goal.'),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('evaluation-report-dimension-TASK_ACHIEVEMENT')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('evaluation-report-dimension-CLARITY_COHERENCE')),
      findsOneWidget,
    );
  });

  testWidgets('four scene families keep their history card and report route', (
    tester,
  ) async {
    const cases = [
      (
        id: 'interview',
        sceneType: EvaluationReportSceneType.interview,
        cardTitle: '模拟面试',
        reportTitle: '面试复盘',
      ),
      (
        id: 'ielts-part-1',
        sceneType: EvaluationReportSceneType.ieltsSpeaking,
        cardTitle: 'IELTS 专项',
        reportTitle: 'Part 1 专项复盘',
      ),
      (
        id: 'workplace',
        sceneType: EvaluationReportSceneType.overseasWorkplace,
        cardTitle: '职场英语复盘',
        reportTitle: '职场英语复盘',
      ),
      (
        id: 'daily-life',
        sceneType: EvaluationReportSceneType.overseasDailyLife,
        cardTitle: '日常英语复盘',
        reportTitle: '日常英语复盘',
      ),
    ];

    for (final testCase in cases) {
      final item = _sceneItem(
        id: 'review-v2-${testCase.id}',
        sceneType: testCase.sceneType,
        practiceMode:
            testCase.sceneType == EvaluationReportSceneType.ieltsSpeaking
            ? 'PART_1'
            : 'FULL_SIMULATION',
        scoreability: EvaluationReportScoreability.provisional,
        dimensions: const <EvaluationReportDimension>[],
      );
      final controller = ReviewHistoryController(
        client: _FixedItemsClient(<ReviewHistoryItem>[item]),
      );

      await tester.pumpWidget(
        MaterialApp(home: ReviewPage(historyController: controller)),
      );
      await tester.pumpAndSettle();

      expect(find.text(testCase.cardTitle), findsOneWidget);
      await tester.tap(
        find.byKey(Key('review-history-select-${item.review.id}')),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('evaluation-report-detail-page')),
        findsOneWidget,
      );
      expect(find.text(testCase.reportTitle), findsWidgets);

      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
    }
  });

  testWidgets('history filters keep all four scene families available', (
    tester,
  ) async {
    const cases = [
      (
        id: 'filter-interview',
        sceneType: EvaluationReportSceneType.interview,
        label: '面试',
      ),
      (
        id: 'filter-ielts',
        sceneType: EvaluationReportSceneType.ieltsSpeaking,
        label: '雅思',
      ),
      (
        id: 'filter-daily-life',
        sceneType: EvaluationReportSceneType.overseasDailyLife,
        label: '日常英语',
      ),
      (
        id: 'filter-workplace',
        sceneType: EvaluationReportSceneType.overseasWorkplace,
        label: '职场英语',
      ),
    ];
    final items = [
      for (final testCase in cases)
        _sceneItem(
          id: testCase.id,
          sceneType: testCase.sceneType,
          scoreability: EvaluationReportScoreability.provisional,
          dimensions: const <EvaluationReportDimension>[],
        ),
    ];
    final controller = ReviewHistoryController(
      client: _FixedItemsClient(items),
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: controller)),
    );
    await tester.pumpAndSettle();

    for (final testCase in cases) {
      expect(find.text(testCase.label), findsOneWidget);
    }

    for (final selectedCase in cases) {
      await tester.tap(find.text(selectedCase.label));
      await tester.pumpAndSettle();

      for (final testCase in cases) {
        expect(
          find.byKey(Key('review-history-${testCase.id}')),
          testCase == selectedCase ? findsOneWidget : findsNothing,
        );
      }
    }
  });

  testWidgets('insufficient evidence never renders a zero score', (
    tester,
  ) async {
    final item = _sceneItem(
      id: 'review-v2-insufficient',
      sceneType: EvaluationReportSceneType.overseasWorkplace,
      scoreability: EvaluationReportScoreability.insufficient,
      dimensions: const <EvaluationReportDimension>[
        EvaluationReportDimension(
          key: 'WORKPLACE_CLARITY',
          scale: EvaluationReportScoreScale.percentage100,
          coverage: 0,
          confidence: 0,
          reasonCodes: <String>['INSUFFICIENT_EVIDENCE'],
          evidenceRefIds: <String>[],
          strengths: <EvaluationReportFinding>[],
          improvements: <EvaluationReportFinding>[],
          recommendedExamples: <EvaluationReportFinding>[],
        ),
      ],
    );
    final controller = ReviewHistoryController(
      client: _FixedItemsClient(<ReviewHistoryItem>[item]),
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: controller)),
    );
    await tester.pumpAndSettle();
    await _expandHistory(tester);
    await tester.tap(find.byKey(const Key('review-toggle-insufficient')));
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(Key('review-history-select-${item.review.id}')),
    );
    await tester.pumpAndSettle();

    expect(find.text('本次暂不评分'), findsOneWidget);
    expect(find.textContaining('有效证据不足'), findsOneWidget);
    expect(find.textContaining('0 / 100'), findsNothing);
    expect(
      find.byKey(const Key('evaluation-report-dimensions')),
      findsOneWidget,
    );
  });

  testWidgets('history card exposes one consolidated semantics node', (
    tester,
  ) async {
    final semanticsHandle = tester.ensureSemantics();
    final item = _fixtureItem(index: 0);
    final controller = ReviewHistoryController(
      client: _FixedItemsClient(<ReviewHistoryItem>[item]),
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: controller)),
    );
    await tester.pumpAndSettle();
    await _expandHistory(tester);

    final expectedLabel =
        '模拟面试，Interview，摘要：${item.review.summary}，'
        '7月26日，已完成，查看复盘详情';
    expect(
      tester.getSemantics(find.byKey(const Key('review-content'))),
      matchesSemantics(
        label: expectedLabel,
        isButton: true,
        hasTapAction: true,
      ),
    );
    expect(find.bySemanticsLabel(RegExp('模拟面试')), findsOneWidget);
    semanticsHandle.dispose();
  });

  testWidgets('ten compact history cards preserve multi-day dates', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(430, 1800));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final items = List<ReviewHistoryItem>.generate(
      10,
      (index) => _fixtureItem(
        index: index,
        completedAt: DateTime(2026, 7, 26 - index, 12),
      ),
    );
    final controller = ReviewHistoryController(
      client: _FixedItemsClient(items),
    );
    addTearDown(controller.dispose);

    await tester.pumpWidget(
      MaterialApp(home: ReviewPage(historyController: controller)),
    );
    await tester.pumpAndSettle();
    await _expandHistory(tester);

    for (final item in items) {
      expect(
        find.byKey(Key('review-history-${item.review.id}')),
        findsOneWidget,
      );
    }
    expect(find.textContaining('7月26日'), findsOneWidget);
    expect(find.textContaining('7月17日'), findsOneWidget);
    expect(
      find.byKey(const Key('evaluation-report-detail-page')),
      findsNothing,
    );
    expect(find.text(items.first.review.summary), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('twenty-five records, long copy, and 2x text stay scrollable', (
    tester,
  ) async {
    await tester.binding.setSurfaceSize(const Size(320, 640));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final longTitle = List<String>.filled(8, '跨团队英文系统设计面试复盘').join(' ');
    final longSummary = List<String>.filled(24, '回答先说明背景与约束，再给出取舍和结果。').join();
    final longStrength = List<String>.filled(20, '能够用具体证据支撑判断，并保持结构清楚。').join();
    final longFocus = List<String>.filled(20, '下一次缩短开场，并更早量化业务影响。').join();
    final items = List<ReviewHistoryItem>.generate(
      25,
      (index) => _fixtureItem(
        index: index,
        title: index == 24 ? longTitle : null,
        summary: index == 24 ? longSummary : null,
        strength: index == 24 ? longStrength : null,
        nextFocus: index == 24 ? longFocus : null,
        completedAt: DateTime(2026, 7, 26).subtract(Duration(days: index)),
      ),
    );
    final controller = ReviewHistoryController(
      client: _FixedItemsClient(items),
    );
    addTearDown(controller.dispose);
    final last = items.last;

    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) {
          final mediaQuery = MediaQuery.of(context);
          return MediaQuery(
            data: mediaQuery.copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          );
        },
        home: ReviewPage(historyController: controller),
      ),
    );
    await tester.pumpAndSettle();
    await _expandHistory(tester);

    final historyScrollable = find
        .descendant(
          of: find.byKey(const Key('review-history-list')),
          matching: find.byType(Scrollable),
        )
        .first;
    await tester.scrollUntilVisible(
      find.byKey(const Key('review-history-load-more')),
      500,
      scrollable: historyScrollable,
    );
    await tester.tap(find.byKey(const Key('review-history-load-more')));
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.byKey(Key('review-history-select-${last.review.id}')),
      500,
      scrollable: historyScrollable,
    );
    await tester.pumpAndSettle();
    expect(find.byKey(Key('review-history-${last.review.id}')), findsOneWidget);
    expect(tester.takeException(), isNull);

    await tester.tap(
      find.byKey(Key('review-history-select-${last.review.id}')),
    );
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('evaluation-report-detail-page')),
      findsOneWidget,
    );
    final detailScrollable = find.descendant(
      of: find.byKey(const Key('evaluation-report-detail-scroll')),
      matching: find.byType(Scrollable),
    );
    await tester.scrollUntilVisible(
      find.byKey(const Key('evaluation-report-dimension-INTERVIEW_STRUCTURE')),
      300,
      scrollable: detailScrollable,
    );
    await tester.pumpAndSettle();
    expect(find.text(longSummary), findsNothing);
    expect(find.text(longFocus), findsWidgets);
    expect(tester.takeException(), isNull);
  });
}

Future<void> _expandHistory(WidgetTester tester) async {
  await tester.pumpAndSettle();
  expect(find.byKey(const Key('review-page')), findsOneWidget);
}

Future<void> _ensureHistoryVisible(WidgetTester tester, Finder target) async {
  final scrollable = find
      .descendant(
        of: find.byKey(const Key('review-history-list')),
        matching: find.byType(Scrollable),
      )
      .first;
  await tester.scrollUntilVisible(target, 200, scrollable: scrollable);
  await tester.pumpAndSettle();
}

Future<HttpServer> _startReviewHistoryServer(
  Future<void> Function(HttpRequest request) respond,
) async {
  final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
  server.listen((request) {
    unawaited(() async {
      try {
        await respond(request);
      } on Object {
        // Rejection intentionally aborts the response while the local server
        // may still be writing the oversized fixture.
      }
    }());
  });
  return server;
}

Future<void> _withRealHttp(Future<void> Function() action) {
  return HttpOverrides.runWithHttpOverrides<Future<void>>(
    action,
    _RealHttpOverrides(),
  );
}

final class _RealHttpOverrides extends HttpOverrides {}

WireReviewHistoryClient _defaultTransportClient(HttpServer server) {
  return WireReviewHistoryClient(
    baseUri: Uri(
      scheme: 'http',
      host: InternetAddress.loopbackIPv4.address,
      port: server.port,
    ),
    credentialProvider: () => const AuthSessionCredential(
      sessionToken: 'sess_review-history-bounds',
      generation: 1,
    ),
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) async {},
  );
}

final Matcher _invalidHistoryResponse = isA<ReviewHistoryException>().having(
  (error) => error.kind,
  'kind',
  ReviewHistoryFailureKind.invalidResponse,
);

const _testMaximumHistoryResponseBytes = 1024 * 1024;

final class _Transport implements IdentityHttpTransport {
  _Transport(this.response);

  final IdentityHttpResponse response;
  late Uri uri;
  late String? authorization;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
    List<int>? bodyBytes,
  }) async {
    expect(method, 'GET');
    this.uri = uri;
    authorization = headers[HttpHeaders.authorizationHeader];
    return response;
  }
}

final class _ControlledClient implements ReviewHistoryClient {
  final _completion = Completer<ReviewHistoryPage>();

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) =>
      _completion.future;

  @override
  Future<void> clearAccountState() async {}

  void complete(ReviewHistoryPage page) => _completion.complete(page);
}

final class _SequencedControlledClient implements ReviewHistoryClient {
  final List<_SequencedRequest> requests = <_SequencedRequest>[];
  int clearCount = 0;

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) {
    final request = _SequencedRequest(cursor);
    requests.add(request);
    return request.completion.future;
  }

  @override
  Future<void> clearAccountState() async {
    clearCount++;
  }

  void complete(int index, ReviewHistoryPage page) {
    requests[index].completion.complete(page);
  }

  void fail(int index, ReviewHistoryException error) {
    requests[index].completion.completeError(error);
  }
}

final class _SequencedRequest {
  _SequencedRequest(this.cursor);

  final String? cursor;
  final Completer<ReviewHistoryPage> completion =
      Completer<ReviewHistoryPage>();
}

final class _PagedClient implements ReviewHistoryClient {
  var calls = 0;

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async {
    calls++;
    await Future<void>.delayed(const Duration(milliseconds: 1));
    if (cursor == null) {
      return ReviewHistoryPage(
        items: [_item(_newerId, score: 91), _item(_olderId, score: 78)],
        nextCursor: 'older_cursor',
      );
    }
    expect(cursor, 'older_cursor');
    return ReviewHistoryPage(items: [_item(_oldestId, score: 70)]);
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _RefreshRetryClient implements ReviewHistoryClient {
  final cursors = <String?>[];

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async {
    cursors.add(cursor);
    await Future<void>.delayed(const Duration(milliseconds: 1));
    switch (cursors.length) {
      case 1:
        return ReviewHistoryPage(
          items: [_item(_newerId, score: 91)],
          nextCursor: 'older_cursor',
        );
      case 2:
        throw const ReviewHistoryException(
          kind: ReviewHistoryFailureKind.network,
          retryable: true,
        );
      default:
        return ReviewHistoryPage(items: [_item(_newerId, score: 92)]);
    }
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _LoadMoreRetryClient implements ReviewHistoryClient {
  final cursors = <String?>[];

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async {
    cursors.add(cursor);
    await Future<void>.delayed(const Duration(milliseconds: 1));
    if (cursors.length == 1) {
      return ReviewHistoryPage(
        items: [_item(_newerId, score: 91)],
        nextCursor: 'older_cursor',
      );
    }
    expect(cursor, 'older_cursor');
    if (cursors.length == 2) {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.network,
        retryable: true,
      );
    }
    return ReviewHistoryPage(items: [_item(_olderId, score: 78)]);
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _FailOnceClient implements ReviewHistoryClient {
  var calls = 0;

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async {
    calls++;
    await Future<void>.delayed(const Duration(milliseconds: 1));
    if (calls == 1) {
      throw const ReviewHistoryException(
        kind: ReviewHistoryFailureKind.network,
        retryable: true,
      );
    }
    return ReviewHistoryPage(items: [_item(_newerId, score: 91)]);
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _AlwaysFailClient implements ReviewHistoryClient {
  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async {
    throw const ReviewHistoryException(
      kind: ReviewHistoryFailureKind.network,
      retryable: true,
    );
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _EmptyClient implements ReviewHistoryClient {
  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async =>
      const ReviewHistoryPage(items: <ReviewHistoryItem>[]);

  @override
  Future<void> clearAccountState() async {}
}

final class _FixedItemsClient implements ReviewHistoryClient {
  const _FixedItemsClient(this.items);

  final List<ReviewHistoryItem> items;

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async {
    if (cursor == null) {
      return ReviewHistoryPage(
        items: items.take(limit).toList(growable: false),
        nextCursor: items.length > limit ? 'fixed-items-page-2' : null,
      );
    }
    expect(cursor, 'fixed-items-page-2');
    return ReviewHistoryPage(
      items: items.skip(limit).take(limit).toList(growable: false),
    );
  }

  @override
  Future<void> clearAccountState() async {}
}

Future<_ReviewShellHarness> _completedShellHarness(String identity) async {
  final harness = _configuredCompletedShellHarness(identity);
  await _restoreCompletedPractice(harness, identity);
  return harness;
}

_ReviewShellHarness _configuredCompletedShellHarness(String identity) {
  final scene = testScenes.first;
  final sessionId = _reviewSessionId(identity);
  final conversation = ConversationController(client: FakeAgentClient());
  return _ReviewShellHarness(
    conversation: conversation,
    composer: ComposerController(conversationController: conversation),
    practice: PracticeController(
      client: FakePracticeClient(
        practiceExperience: scene.experience,
        sceneCategory: scene.category,
        initialSnapshot: testPracticeSnapshot(
          scene: scene,
          sessionId: sessionId,
          completedTurns: 3,
        ),
      ),
    ),
  );
}

Future<void> _restoreCompletedPractice(
  _ReviewShellHarness harness,
  String identity,
) async {
  final scene = testScenes.first;
  await harness.conversation.initialize();
  await harness.practice.restoreCreatedPractice(
    sessionId: _reviewSessionId(identity),
    scene: scene,
  );
}

final class _ReviewShellHarness {
  const _ReviewShellHarness({
    required this.conversation,
    required this.composer,
    required this.practice,
  });

  final ConversationController conversation;
  final ComposerController composer;
  final PracticeController practice;

  void dispose() {
    composer.dispose();
    conversation.dispose();
    practice.dispose();
  }
}

String _reviewSessionId(String reviewId) => 'session-$reviewId';

ReviewHistoryItem _item(String id, {required int score}) {
  final createdAt = DateTime.utc(2026, 7, 26, 10, score % 60);
  final completedAt = createdAt.add(const Duration(minutes: 1));
  final review = ReviewSummary(
    id: id,
    title: '本次练习 · $score 分',
    summary: 'summary-$score',
    strength: 'strength-$score',
    nextFocus: 'focus-$score',
  );
  return ReviewHistoryItem(
    review: review,
    report: evaluationReportFixture(
      review: review,
      practiceSessionId: 'session-$score',
      completedAt: completedAt,
      score: score.toDouble(),
    ),
    practiceSessionId: 'session-$score',
    createdAt: createdAt,
    completedAt: completedAt,
  );
}

ReviewHistoryItem _sceneItem({
  required String id,
  required EvaluationReportSceneType sceneType,
  required EvaluationReportScoreability scoreability,
  required List<EvaluationReportDimension> dimensions,
  String? practiceMode,
  String? summary,
  List<EvaluationReportPriorityAction> priorityActions =
      const <EvaluationReportPriorityAction>[],
}) {
  final createdAt = DateTime.utc(2026, 7, 30, 3);
  final completedAt = createdAt.add(const Duration(minutes: 2));
  final report = EvaluationReport(
    id: id,
    evaluationId: '7b000001-0000-4000-8000-000000000001',
    practiceSessionId: 'session-$id',
    sceneType: sceneType,
    practiceExperience: switch (sceneType) {
      EvaluationReportSceneType.ieltsSpeaking => 'IELTS_SPEAKING',
      EvaluationReportSceneType.interview => 'INTERVIEW',
      EvaluationReportSceneType.overseasDailyLife => 'LIFE_AND_TRAVEL',
      EvaluationReportSceneType.overseasWorkplace => 'WORKPLACE',
    },
    sceneCategory: switch (sceneType) {
      EvaluationReportSceneType.ieltsSpeaking => 'IELTS_SPEAKING',
      EvaluationReportSceneType.interview => 'INTERVIEW_PROFESSIONAL',
      EvaluationReportSceneType.overseasDailyLife => 'LIFE_TRAVEL',
      EvaluationReportSceneType.overseasWorkplace => 'WORKPLACE_GENERAL',
    },
    practiceMode:
        practiceMode ??
        (sceneType == EvaluationReportSceneType.ieltsSpeaking
            ? 'FULL_MOCK'
            : 'FULL_SIMULATION'),
    scoreability: scoreability,
    summary:
        summary ??
        (scoreability == EvaluationReportScoreability.insufficient
            ? '当前回答不足以形成可靠结论。'
            : '本次回答已经形成可复盘的文本反馈。'),
    questions: const <EvaluationReportQuestion>[
      EvaluationReportQuestion(
        id: '40000000-0000-4000-8000-000000000001',
        position: 1,
        text: 'Tell me about your experience.',
        answer: EvaluationReportAnswer(
          turnId: '50000000-0000-4000-8000-000000000005',
          transcript: 'I led a project and improved delivery.',
        ),
      ),
    ],
    dimensions: dimensions,
    priorityActions: priorityActions,
    createdAt: completedAt,
  );
  return ReviewHistoryItem(
    review: presentEvaluationReport(report),
    report: report,
    practiceSessionId: report.practiceSessionId,
    createdAt: createdAt,
    completedAt: completedAt,
  );
}

EvaluationReportDimension _part1Dimension({
  required String key,
  required double score,
  EvaluationReportFinding? improvement,
  List<EvaluationReportFinding>? improvements,
  String? improvementMessage,
}) {
  final finding =
      improvement ??
      EvaluationReportFinding(
        id: '$key-improvement',
        message: improvementMessage ?? '$key improvement',
        suggestion: '$key suggestion',
        evidence: const <EvaluationReportEvidence>[],
      );
  return EvaluationReportDimension(
    key: key,
    score: score,
    scale: EvaluationReportScoreScale.ieltsBand,
    coverage: 1,
    confidence: 0.8,
    reasonCodes: const <String>[],
    evidenceRefIds: const <String>[],
    strengths: const <EvaluationReportFinding>[],
    improvements: improvements ?? <EvaluationReportFinding>[finding],
    recommendedExamples: const <EvaluationReportFinding>[],
  );
}

ReviewHistoryItem _fixtureItem({
  required int index,
  DateTime? completedAt,
  String? title,
  String? summary,
  String? strength,
  String? nextFocus,
}) {
  final completed = completedAt ?? DateTime(2026, 7, 26, 12, index);
  final created = completed.subtract(const Duration(minutes: 16));
  final review = ReviewSummary(
    id: 'review-fixture-$index',
    title: title ?? '英文面试练习 · ${90 - index} 分',
    summary: summary ?? 'fixture-summary-$index',
    strength: strength ?? 'fixture-strength-$index',
    nextFocus: nextFocus ?? 'fixture-focus-$index',
  );
  return ReviewHistoryItem(
    review: review,
    report: evaluationReportFixture(
      review: review,
      practiceSessionId: 'session-fixture-$index',
      completedAt: completed,
      score: (90 - index).toDouble(),
    ),
    practiceSessionId: 'session-fixture-$index',
    createdAt: created,
    completedAt: completed,
  );
}

Map<String, Object?> _wireItem({
  required String id,
  required String createdAt,
  required int score,
}) {
  return evaluationReportWireFixture(
    reportId: id,
    practiceSessionId:
        '30000000-0000-4000-8000-${score.toString().padLeft(12, '0')}',
    createdAt: createdAt,
    score: score.toDouble(),
  );
}

const _newerId = '20000000-0000-4000-8000-000000000003';
const _olderId = '20000000-0000-4000-8000-000000000002';
const _oldestId = '20000000-0000-4000-8000-000000000001';
