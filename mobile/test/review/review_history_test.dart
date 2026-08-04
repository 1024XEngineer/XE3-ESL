import '../support/scene_fixtures.dart';
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_controller.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/app/speak_up_shell.dart';
import 'package:speakup/features/review/review.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';
import 'package:speakup/practice/practice_client.dart';
import 'package:speakup/review/formal_review.dart';
import 'package:speakup/review/formal_review_presentation.dart';
import 'package:speakup/review/review_history_client.dart';
import 'package:speakup/review/review_history_controller.dart';
import 'package:speakup/review/wire_review_history_client.dart';

import 'formal_review_fixture.dart';
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
    expect(page.items.first.review.title, '本次练习 · 91 分');
    expect(
      page.items.first.formalReview.schema,
      FormalReviewSchema.legacyVoiceV1,
    );
    expect(page.items.first.formalReview.result?.overallScore, 91);
    expect(page.nextCursor, cursor);
    expect(transport.uri.path, '/v1/formal-reviews');
    expect(transport.uri.queryParameters, {'limit': '2'});
    expect(transport.authorization, 'Bearer sess_review-history');
  });

  test('wire decoder accepts exact UTF-8 field budgets', () async {
    final result = <String, Object?>{
      'overall_score': 93,
      'summary': _utf8Text(_testMaximumReviewTextBytes),
      'conclusions': <Object?>[
        <String, Object?>{
          'key': _utf8Text(_testMaximumReviewLabelBytes),
          'category': _utf8Text(_testMaximumReviewLabelBytes),
          'message': _utf8Text(_testMaximumReviewTextBytes),
          'suggestion': _utf8Text(_testMaximumReviewTextBytes),
        },
      ],
    };
    expect(
      utf8.encode(jsonEncode(result)).length,
      lessThan(_testMaximumReviewResultBytes),
    );
    final item = _wireItem(
      id: _newerId,
      createdAt: '2026-07-26T10:00:00Z',
      score: 93,
      practiceSessionId: _utf8Text(_testMaximumReviewMetadataBytes),
      implementationVersion: _utf8Text(_testMaximumReviewMetadataBytes),
      sourceTurnId: _utf8Text(_testMaximumReviewMetadataBytes),
      sourceTurnVersion: _sourceVersionAtBytes(_testMaximumReviewMetadataBytes),
      result: result,
    );

    final page = await _wireClientForBody(
      jsonEncode({
        'items': [item],
      }),
    ).list(limit: 1);

    expect(page.items, hasLength(1));
    expect(
      utf8.encode(page.items.single.practiceSessionId).length,
      _testMaximumReviewMetadataBytes,
    );
    expect(
      utf8.encode(page.items.single.review.summary).length,
      _testMaximumReviewTextBytes,
    );
    expect(
      utf8.encode(page.items.single.review.strength).length,
      _testMaximumReviewTextBytes,
    );
    expect(
      utf8.encode(page.items.single.review.nextFocus).length,
      _testMaximumReviewTextBytes,
    );
  });

  test('wire decoder accepts a Result encoded at exactly 12 KiB', () async {
    final result = _resultAtEncodedByteSize(_testMaximumReviewResultBytes);
    final item = _wireItem(
      id: _newerId,
      createdAt: '2026-07-26T10:00:00Z',
      score: 93,
      result: result,
    );

    final page = await _wireClientForBody(
      jsonEncode({
        'items': [item],
      }),
    ).list(limit: 1);

    expect(page.items, hasLength(1));
    expect(
      utf8.encode(jsonEncode(result)).length,
      _testMaximumReviewResultBytes,
    );
  });

  test('wire decoder rejects every UTF-8 field above its budget', () async {
    final cases =
        <({String name, void Function(Map<String, Object?> item) mutate})>[
          (
            name: 'practice_session_id',
            mutate: (item) {
              item['practice_session_id'] = _utf8Text(
                _testMaximumReviewMetadataBytes + 1,
              );
            },
          ),
          (
            name: 'implementation_version',
            mutate: (item) {
              item['implementation_version'] = _utf8Text(
                _testMaximumReviewMetadataBytes + 1,
              );
            },
          ),
          (
            name: 'source_turn_id',
            mutate: (item) {
              item['source_turn_id'] = _utf8Text(
                _testMaximumReviewMetadataBytes + 1,
              );
            },
          ),
          (
            name: 'source_turn_version',
            mutate: (item) {
              item['source_turn_version'] = _sourceVersionAtBytes(
                _testMaximumReviewMetadataBytes + 1,
              );
            },
          ),
          (
            name: 'summary',
            mutate: (item) {
              _wireResult(item)['summary'] = _utf8Text(
                _testMaximumReviewTextBytes + 1,
              );
            },
          ),
          (
            name: 'conclusion key',
            mutate: (item) {
              _wireConclusion(item)['key'] = _utf8Text(
                _testMaximumReviewLabelBytes + 1,
              );
            },
          ),
          (
            name: 'conclusion category',
            mutate: (item) {
              _wireConclusion(item)['category'] = _utf8Text(
                _testMaximumReviewLabelBytes + 1,
              );
            },
          ),
          (
            name: 'conclusion message',
            mutate: (item) {
              _wireConclusion(item)['message'] = _utf8Text(
                _testMaximumReviewTextBytes + 1,
              );
            },
          ),
          (
            name: 'conclusion suggestion',
            mutate: (item) {
              _wireConclusion(item)['suggestion'] = _utf8Text(
                _testMaximumReviewTextBytes + 1,
              );
            },
          ),
        ];

    for (final testCase in cases) {
      final item = _wireItem(
        id: _newerId,
        createdAt: '2026-07-26T10:00:00Z',
        score: 93,
      );
      testCase.mutate(item);

      await expectLater(
        _wireClientForBody(
          jsonEncode({
            'items': [item],
          }),
        ).list(limit: 1),
        throwsA(_invalidHistoryResponse),
        reason: testCase.name,
      );
    }
  });

  test('wire decoder rejects NUL in every bounded string field', () async {
    final cases =
        <({String name, void Function(Map<String, Object?> item) mutate})>[
          (
            name: 'practice_session_id metadata',
            mutate: (item) {
              item['practice_session_id'] = 'practice\u0000session';
            },
          ),
          (
            name: 'implementation_version metadata',
            mutate: (item) {
              item['implementation_version'] = 'review\u0000v1';
            },
          ),
          (
            name: 'source_turn_id metadata',
            mutate: (item) {
              item['source_turn_id'] = 'turn\u0000id';
            },
          ),
          (
            name: 'source_turn_version metadata',
            mutate: (item) {
              item['source_turn_version'] =
                  'conversation-turn:evidence-v1\u0000';
            },
          ),
          (
            name: 'summary',
            mutate: (item) {
              _wireResult(item)['summary'] = 'summary\u0000text';
            },
          ),
          (
            name: 'conclusion key',
            mutate: (item) {
              _wireConclusion(item)['key'] = 'clarity\u0000key';
            },
          ),
          (
            name: 'conclusion category',
            mutate: (item) {
              _wireConclusion(item)['category'] = 'clarity\u0000category';
            },
          ),
          (
            name: 'conclusion message',
            mutate: (item) {
              _wireConclusion(item)['message'] = 'message\u0000text';
            },
          ),
          (
            name: 'conclusion suggestion',
            mutate: (item) {
              _wireConclusion(item)['suggestion'] = 'suggestion\u0000text';
            },
          ),
        ];

    for (final testCase in cases) {
      final item = _wireItem(
        id: _newerId,
        createdAt: '2026-07-26T10:00:00Z',
        score: 93,
      );
      testCase.mutate(item);

      await expectLater(
        _wireClientForBody(
          jsonEncode({
            'items': [item],
          }),
        ).list(limit: 1),
        throwsA(_invalidHistoryResponse),
        reason: testCase.name,
      );
    }
  });

  test('wire decoder rejects a present empty suggestion', () async {
    for (final value in <String>['', '   ']) {
      final item = _wireItem(
        id: _newerId,
        createdAt: '2026-07-26T10:00:00Z',
        score: 93,
      );
      _wireConclusion(item)['suggestion'] = value;

      await expectLater(
        _wireClientForBody(
          jsonEncode({
            'items': [item],
          }),
        ).list(limit: 1),
        throwsA(_invalidHistoryResponse),
      );
    }
  });

  test('wire decoder rejects nine conclusions', () async {
    final item = _wireItem(
      id: _newerId,
      createdAt: '2026-07-26T10:00:00Z',
      score: 93,
    );
    _wireResult(item)['conclusions'] = List<Object?>.generate(
      _testMaximumReviewConclusions + 1,
      (index) => <String, Object?>{
        'key': 'key-$index',
        'category': 'clarity',
        'message': 'message-$index',
      },
    );

    await expectLater(
      _wireClientForBody(
        jsonEncode({
          'items': [item],
        }),
      ).list(limit: 1),
      throwsA(_invalidHistoryResponse),
    );
  });

  test('wire decoder rejects a Result over 12 KiB with valid fields', () async {
    final result = _resultAtEncodedByteSize(_testMaximumReviewResultBytes);
    final firstConclusion =
        (result['conclusions']! as List<Object?>).first as Map<String, Object?>;
    firstConclusion['suggestion'] =
        '${firstConclusion['suggestion'] as String}a';
    expect(
      utf8.encode(jsonEncode(result)).length,
      _testMaximumReviewResultBytes + 1,
    );
    expect(
      utf8.encode(firstConclusion['suggestion'] as String).length,
      lessThanOrEqualTo(_testMaximumReviewTextBytes),
    );
    final item = _wireItem(
      id: _newerId,
      createdAt: '2026-07-26T10:00:00Z',
      score: 93,
      result: result,
    );

    await expectLater(
      _wireClientForBody(
        jsonEncode({
          'items': [item],
        }),
      ).list(limit: 1),
      throwsA(_invalidHistoryResponse),
    );
  });

  test(
    'default transport accepts 50 maximum-budget Results below 1 MiB',
    () async {
      await _withRealHttp(() async {
        final result = _resultAtEncodedByteSize(_testMaximumReviewResultBytes);
        final baseTime = DateTime.utc(2026, 7, 26, 10);
        final items = List<Map<String, Object?>>.generate(50, (index) {
          return _wireItem(
            id: _wireReviewId(50 - index),
            createdAt: baseTime
                .subtract(Duration(minutes: index))
                .toIso8601String(),
            score: 90,
            result: result,
          );
        });
        final body = jsonEncode({'items': items});
        final bytes = utf8.encode(body);
        expect(bytes.length, greaterThan(600 * 1024));
        expect(bytes.length, lessThan(_testMaximumHistoryResponseBytes));
        final server = await _startReviewHistoryServer((request) async {
          request.response.contentLength = bytes.length;
          request.response.add(bytes);
          await request.response.close();
        });
        addTearDown(() => server.close(force: true));

        final page = await _defaultTransportClient(server).list(limit: 50);

        expect(page.items, hasLength(50));
        expect(page.items.first.review.id, _wireReviewId(50));
        expect(page.items.last.review.id, _wireReviewId(1));
      });
    },
  );

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

  testWidgets(
    'Shell init refreshes history once for a preloaded current Review',
    (tester) async {
      final client = _SequencedControlledClient();
      final historyController = ReviewHistoryController(client: client);
      final agentController = await _agentControllerWithReview(_newerId);
      addTearDown(historyController.dispose);
      addTearDown(agentController.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: SpeakUpShell(
            agentController: agentController,
            reviewHistoryController: historyController,
          ),
        ),
      );
      await tester.pump();

      expect(client.requests, hasLength(1));
      expect(client.requests.single.cursor, isNull);
      expect(
        find.byKey(const Key('review-content')).hitTestable(),
        findsOneWidget,
      );

      client.complete(
        0,
        ReviewHistoryPage(items: [_item(_olderId, score: 78)]),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('review-history-$_olderId')), findsOneWidget);
      expect(client.requests, hasLength(1));
    },
  );

  testWidgets(
    'Shell refreshes history once when Agent restore presents a late Review',
    (tester) async {
      final client = _SequencedControlledClient();
      final historyController = ReviewHistoryController(client: client);
      final agentController = _configuredAgentControllerWithReview(_newerId);
      addTearDown(historyController.dispose);
      addTearDown(agentController.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: SpeakUpShell(
            agentController: agentController,
            reviewHistoryController: historyController,
          ),
        ),
      );
      await tester.pump();

      expect(client.requests, isEmpty);
      expect(find.byKey(const Key('agent-home-page')), findsOneWidget);

      await _restoreConfiguredReview(agentController, _newerId);
      await tester.pump();

      expect(client.requests, hasLength(1));
      expect(client.requests.single.cursor, isNull);
      expect(
        find.byKey(const Key('review-content')).hitTestable(),
        findsOneWidget,
      );

      client.complete(0, const ReviewHistoryPage(items: []));
      await tester.pumpAndSettle();

      expect(client.requests, hasLength(1));
      expect(find.byKey(const Key('review-current-$_newerId')), findsOneWidget);
    },
  );

  testWidgets(
    'Shell widget updates coalesce one refresh without rebuild storms',
    (tester) async {
      final firstClient = _SequencedControlledClient();
      final firstHistoryController = ReviewHistoryController(
        client: firstClient,
      );
      final secondClient = _SequencedControlledClient();
      final secondHistoryController = ReviewHistoryController(
        client: secondClient,
      );
      final firstAgentController = await _agentControllerWithReview(_newerId);
      final secondAgentController = await _agentControllerWithReview(_olderId);
      addTearDown(firstHistoryController.dispose);
      addTearDown(secondHistoryController.dispose);
      addTearDown(firstAgentController.dispose);
      addTearDown(secondAgentController.dispose);

      var agentController = firstAgentController;
      var historyController = firstHistoryController;
      late StateSetter rebuild;
      await tester.pumpWidget(
        MaterialApp(
          home: StatefulBuilder(
            builder: (context, setState) {
              rebuild = setState;
              return SpeakUpShell(
                agentController: agentController,
                reviewHistoryController: historyController,
              );
            },
          ),
        ),
      );
      await tester.pump();

      expect(firstClient.requests, hasLength(1));

      for (var index = 0; index < 5; index++) {
        rebuild(() {});
        await tester.pump();
      }
      expect(firstClient.requests, hasLength(1));

      rebuild(() => agentController = secondAgentController);
      await tester.pump();
      for (var index = 0; index < 5; index++) {
        rebuild(() {});
        await tester.pump();
      }

      expect(firstClient.requests, hasLength(1));
      firstClient.complete(0, const ReviewHistoryPage(items: []));
      await tester.pump();

      expect(firstClient.requests, hasLength(2));
      expect(firstClient.requests.last.cursor, isNull);
      firstClient.complete(1, const ReviewHistoryPage(items: []));
      await tester.pump();

      rebuild(() => historyController = secondHistoryController);
      await tester.pump();
      for (var index = 0; index < 5; index++) {
        rebuild(() {});
        await tester.pump();
      }

      expect(secondClient.requests, hasLength(1));
      expect(secondClient.requests.single.cursor, isNull);
      secondClient.complete(0, const ReviewHistoryPage(items: []));
      await tester.pumpAndSettle();

      expect(secondClient.requests, hasLength(1));
      expect(
        find.byKey(const Key('review-current-$_olderId')).hitTestable(),
        findsOneWidget,
      );
    },
  );

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
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('review-content')), findsOneWidget);
      expect(find.byKey(const Key('review-title')), findsOneWidget);
      expect(find.text('summary-91'), findsOneWidget);
      expect(find.byKey(const Key('review-history-load-more')), findsOneWidget);

      await tester.tap(
        find.byKey(const Key('review-history-select-$_newerId')),
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('review-detail-page')), findsOneWidget);
      expect(find.text('summary-91'), findsOneWidget);
      expect(find.text('summary-78'), findsNothing);

      await tester.tap(find.byKey(const Key('review-detail-back')));
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const Key('review-history-select-$_olderId')),
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('review-detail-page')), findsOneWidget);
      expect(find.text('summary-78'), findsOneWidget);
      expect(find.text('summary-91'), findsNothing);

      await tester.tap(find.byKey(const Key('review-detail-back')));
      await tester.pumpAndSettle();
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
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('review-history-error')), findsOneWidget);

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

      await tester.tap(find.byKey(const Key('review-history-page-retry')));
      await tester.pumpAndSettle();

      expect(client.cursors, <String?>[null, null, null]);
      expect(find.text('本次练习 · 92 分'), findsOneWidget);
      expect(find.text('summary-92'), findsOneWidget);
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

    await tester.tap(find.byKey(const Key('review-history-load-more')));
    await tester.pumpAndSettle();

    expect(client.cursors, <String?>[null, 'older_cursor']);
    expect(find.byKey(const Key('review-history-page-error')), findsOneWidget);

    await tester.tap(find.byKey(const Key('review-history-page-retry')));
    await tester.pumpAndSettle();

    expect(client.cursors, <String?>[null, 'older_cursor', 'older_cursor']);
    expect(find.byKey(const Key('review-history-$_olderId')), findsOneWidget);
    expect(controller.hasMore, isFalse);
  });

  testWidgets(
    'current server Review stays visible while history loads and deduplicates',
    (tester) async {
      final historyClient = _ControlledClient();
      final historyController = ReviewHistoryController(client: historyClient);
      final agentController = await _agentControllerWithReview(_newerId);
      addTearDown(historyController.dispose);
      addTearDown(agentController.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: ReviewPage(
            historyController: historyController,
            agentController: agentController,
          ),
        ),
      );
      await tester.pump();

      expect(find.byKey(const Key('review-content')), findsOneWidget);
      expect(find.byKey(const Key('review-title')), findsOneWidget);
      expect(find.byKey(const Key('review-current-label')), findsOneWidget);
      expect(find.text('本次结果'), findsOneWidget);
      expect(find.textContaining('刚'), findsNothing);
      expect(
        find.byKey(const Key('review-history-page-loading')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('review-history-initial-loading')),
        findsNothing,
      );

      historyClient.complete(
        ReviewHistoryPage(items: [_item(_newerId, score: 91)]),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('review-current-$_newerId')), findsNothing);
      expect(find.byKey(const Key('review-history-$_newerId')), findsOneWidget);
      expect(find.byKey(const Key('review-content')), findsOneWidget);
      expect(find.byKey(const Key('review-title')), findsOneWidget);
    },
  );

  testWidgets(
    'history failure or empty page never hides the current server Review',
    (tester) async {
      final failureController = ReviewHistoryController(
        client: _AlwaysFailClient(),
      );
      final agentController = await _agentControllerWithReview(_newerId);
      addTearDown(failureController.dispose);
      addTearDown(agentController.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: ReviewPage(
            historyController: failureController,
            agentController: agentController,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('review-content')), findsOneWidget);
      expect(
        find.byKey(const Key('review-history-page-error')),
        findsOneWidget,
      );
      expect(find.byKey(const Key('review-history-error')), findsNothing);

      final emptyController = ReviewHistoryController(client: _EmptyClient());
      addTearDown(emptyController.dispose);
      await tester.pumpWidget(
        MaterialApp(
          home: ReviewPage(
            historyController: emptyController,
            agentController: agentController,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('review-content')), findsOneWidget);
      expect(find.byKey(const Key('review-current-$_newerId')), findsOneWidget);
      expect(find.byKey(const Key('review-availability-title')), findsNothing);
    },
  );

  testWidgets(
    'current Review and older history remain distinct selectable results',
    (tester) async {
      final historyController = ReviewHistoryController(
        client: _SinglePageClient(_item(_olderId, score: 78)),
      );
      final agentController = await _agentControllerWithReview(_newerId);
      addTearDown(historyController.dispose);
      addTearDown(agentController.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: ReviewPage(
            historyController: historyController,
            agentController: agentController,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('review-current-$_newerId')), findsOneWidget);
      expect(find.byKey(const Key('review-history-$_olderId')), findsOneWidget);
      expect(find.byKey(const Key('review-content')), findsOneWidget);
      expect(find.text('summary-91'), findsOneWidget);
      expect(find.text('summary-78'), findsOneWidget);

      await tester.tap(
        find.byKey(const Key('review-current-select-$_newerId')),
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('review-detail-page')), findsOneWidget);
      expect(find.text('summary-91'), findsOneWidget);
      expect(find.text('summary-78'), findsNothing);

      await tester.tap(find.byKey(const Key('review-detail-back')));
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const Key('review-history-select-$_olderId')),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('review-detail-page')), findsOneWidget);
      expect(find.byKey(const Key('review-detail-title')), findsOneWidget);
      expect(find.text('summary-91'), findsNothing);
      expect(find.text('summary-78'), findsOneWidget);
    },
  );

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

    expect(find.byKey(Key('review-history-${item.review.id}')), findsOneWidget);
    expect(find.byKey(const Key('review-detail-page')), findsNothing);
    expect(find.text(item.review.summary), findsOneWidget);
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

    expect(find.byKey(const Key('review-detail-page')), findsOneWidget);
    expect(find.byKey(const Key('review-detail-summary')), findsOneWidget);
    expect(find.byKey(const Key('review-detail-strength')), findsOneWidget);
    expect(find.byKey(const Key('review-detail-focus')), findsOneWidget);
    expect(find.text(item.review.summary), findsOneWidget);

    await tester.tap(find.byKey(const Key('review-detail-back')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('review-history-list')), findsOneWidget);
    expect(find.byKey(const Key('review-detail-page')), findsNothing);
  });

  testWidgets(
    'scene v2 detail renders dimensions and corrections without a total',
    (tester) async {
      final item = _sceneItem(
        id: 'review-v2-interview',
        contextType: FormalReviewContextType.interviewProjectDeepDive,
        eligibility: FormalReviewSummaryEligibility.eligible,
        dimensions: const <FormalReviewDimension>[
          FormalReviewDimension(
            key: 'structure',
            category: 'relevance_structure',
            score: 82,
            message: '回答紧扣问题，并按背景、行动、结果展开。',
            suggestion: '开场先用一句话说明最终结果。',
          ),
          FormalReviewDimension(
            key: 'evidence',
            category: 'evidence_impact',
            score: 76,
            message: '给出了结果，但影响范围还不够具体。',
          ),
        ],
        feedbackItems: const <FormalReviewFeedbackItem>[
          FormalReviewFeedbackItem(
            key: 'correction-1',
            kind: FormalReviewFeedbackKind.correction,
            message: 'I responsible for the migration.',
            suggestion: 'I was responsible for the migration.',
          ),
          FormalReviewFeedbackItem(
            key: 'strength-1',
            kind: FormalReviewFeedbackKind.strength,
            message: '用具体故障数量解释了项目影响。',
          ),
        ],
        repracticeSuggestionRefs: const <String>['correction-1'],
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

      expect(find.text('面试复盘'), findsOneWidget);
      expect(find.byKey(const Key('review-detail-dimensions')), findsOneWidget);
      expect(find.text('回答相关性与结构'), findsOneWidget);
      expect(find.text('82 / 100'), findsOneWidget);
      expect(find.byKey(const Key('review-detail-feedback')), findsOneWidget);
      expect(find.text('纠错'), findsOneWidget);
      expect(find.text('优先练习'), findsOneWidget);
      expect(
        find.textContaining('I was responsible for the migration.'),
        findsOneWidget,
      );
      expect(find.byKey(const Key('review-detail-strength')), findsNothing);
      expect(find.byKey(const Key('review-detail-focus')), findsNothing);
      expect(find.textContaining('面试复盘 ·'), findsNothing);
    },
  );

  testWidgets(
    'provisional IELTS explains the missing pronunciation and Overall',
    (tester) async {
      final item = _sceneItem(
        id: 'review-v2-ielts',
        contextType: FormalReviewContextType.ieltsSpeakingPart2,
        eligibility: FormalReviewSummaryEligibility.provisional,
        dimensions: const <FormalReviewDimension>[
          FormalReviewDimension(
            key: 'coverage',
            category: 'task_coverage_development',
            score: 74,
            message: '覆盖了题卡的主要提示点。',
          ),
        ],
        insufficientEvidenceReasons: const <String>[
          'pronunciation_audio_evidence_unavailable',
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
      expect(find.text('面试能力反馈'), findsOneWidget);
      await tester.tap(
        find.byKey(Key('review-history-select-${item.review.id}')),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('review-detail-status-notice')),
        findsOneWidget,
      );
      expect(find.textContaining('发音尚未评估'), findsOneWidget);
      expect(find.textContaining('不是 IELTS 官方成绩'), findsOneWidget);
      expect(find.textContaining('Overall'), findsOneWidget);
      expect(find.text('74 / 100'), findsOneWidget);
    },
  );

  testWidgets('insufficient evidence never renders a zero score', (
    tester,
  ) async {
    final item = _sceneItem(
      id: 'review-v2-insufficient',
      contextType: FormalReviewContextType.workplaceProgressRiskUpdate,
      eligibility: FormalReviewSummaryEligibility.insufficientEvidence,
      dimensions: const <FormalReviewDimension>[],
      insufficientEvidenceReasons: const <String>['confirmed_answer_too_short'],
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

    expect(find.text('本次暂不评分'), findsOneWidget);
    expect(find.textContaining('有效回答太短'), findsOneWidget);
    expect(find.textContaining('0 / 100'), findsNothing);
    expect(find.byKey(const Key('review-detail-dimensions')), findsNothing);
    expect(find.byKey(const Key('review-detail-feedback')), findsNothing);
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

    final expectedLabel =
        '${item.review.title}，摘要：${item.review.summary}，'
        '2026-07-26，已完成，查看复盘详情';
    expect(
      tester.getSemantics(find.byKey(const Key('review-content'))),
      matchesSemantics(
        label: expectedLabel,
        isButton: true,
        hasTapAction: true,
      ),
    );
    expect(
      find.bySemanticsLabel(RegExp(RegExp.escape(item.review.title))),
      findsOneWidget,
    );
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

    for (final item in items) {
      expect(
        find.byKey(Key('review-history-${item.review.id}')),
        findsOneWidget,
      );
    }
    expect(find.text('2026-07-26'), findsOneWidget);
    expect(find.text('2026-07-17'), findsOneWidget);
    expect(find.byKey(const Key('review-detail-page')), findsNothing);
    expect(find.text(items.first.review.summary), findsOneWidget);
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

    final historyScrollable = find.descendant(
      of: find.byKey(const Key('review-history-list')),
      matching: find.byType(Scrollable),
    );
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
    expect(find.byKey(const Key('review-detail-page')), findsOneWidget);
    final detailScrollable = find.descendant(
      of: find.byKey(const Key('review-detail-content')),
      matching: find.byType(Scrollable),
    );
    await tester.scrollUntilVisible(
      find.byKey(const Key('review-detail-summary')),
      300,
      scrollable: detailScrollable,
    );
    expect(find.text(longSummary), findsOneWidget);

    await tester.scrollUntilVisible(
      find.byKey(const Key('review-detail-focus')),
      300,
      scrollable: detailScrollable,
    );
    await tester.pumpAndSettle();
    expect(find.text(longFocus), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
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

WireReviewHistoryClient _wireClientForBody(String body) {
  return WireReviewHistoryClient(
    baseUri: Uri.parse('https://api.speak-up.test'),
    credentialProvider: () => const AuthSessionCredential(
      sessionToken: 'sess_review-history-decoder',
      generation: 1,
    ),
    invalidateSession:
        ({required expectedSessionToken, required expectedGeneration}) async {},
    transport: _Transport(
      IdentityHttpResponse(statusCode: HttpStatus.ok, body: body),
    ),
  );
}

final Matcher _invalidHistoryResponse = isA<ReviewHistoryException>().having(
  (error) => error.kind,
  'kind',
  ReviewHistoryFailureKind.invalidResponse,
);

const _testMaximumHistoryResponseBytes = 1024 * 1024;
const _testMaximumReviewResultBytes = 12 * 1024;
const _testMaximumReviewMetadataBytes = 128;
const _testMaximumReviewLabelBytes = 64;
const _testMaximumReviewTextBytes = 2048;
const _testMaximumReviewConclusions = 8;

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

final class _SinglePageClient implements ReviewHistoryClient {
  const _SinglePageClient(this.item);

  final ReviewHistoryItem item;

  @override
  Future<ReviewHistoryPage> list({String? cursor, int limit = 20}) async =>
      ReviewHistoryPage(items: <ReviewHistoryItem>[item]);

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

Future<AgentController> _agentControllerWithReview(String reviewId) async {
  final controller = _configuredAgentControllerWithReview(reviewId);
  await _restoreConfiguredReview(controller, reviewId);
  return controller;
}

AgentController _configuredAgentControllerWithReview(String reviewId) {
  final scene = testScenes.first;
  final sessionId = _reviewSessionId(reviewId);
  return AgentController(
    client: FakeAgentClient(),
    practiceClient: FakePracticeClient(
      sceneFamily: scene.family,
      sceneModel: scene.model,
      initialSnapshot: testPracticeSnapshot(
        scene: scene,
        sessionId: sessionId,
        completedTurns: 3,
        review: AgentReview(
          id: reviewId,
          title: '本次练习 · 91 分',
          summary: 'summary-91',
          strength: 'strength-91',
          nextFocus: 'focus-91',
        ),
      ),
    ),
  );
}

Future<void> _restoreConfiguredReview(
  AgentController controller,
  String reviewId,
) async {
  final scene = testScenes.first;
  await controller.initialize();
  await controller.selectScene(scene);
  await controller.restoreCreatedPractice(
    sessionId: _reviewSessionId(reviewId),
    scene: scene,
  );
}

String _reviewSessionId(String reviewId) => 'session-$reviewId';

ReviewHistoryItem _item(String id, {required int score}) {
  final createdAt = DateTime.utc(2026, 7, 26, 10, score % 60);
  final completedAt = createdAt.add(const Duration(minutes: 1));
  final review = AgentReview(
    id: id,
    title: '本次练习 · $score 分',
    summary: 'summary-$score',
    strength: 'strength-$score',
    nextFocus: 'focus-$score',
  );
  return ReviewHistoryItem(
    review: review,
    formalReview: legacyFormalReviewFixture(
      review: review,
      practiceSessionId: 'session-$score',
      createdAt: createdAt,
      completedAt: completedAt,
      overallScore: score,
    ),
    practiceSessionId: 'session-$score',
    createdAt: createdAt,
    completedAt: completedAt,
  );
}

ReviewHistoryItem _sceneItem({
  required String id,
  required FormalReviewContextType contextType,
  required FormalReviewSummaryEligibility eligibility,
  required List<FormalReviewDimension> dimensions,
  List<FormalReviewFeedbackItem> feedbackItems =
      const <FormalReviewFeedbackItem>[],
  List<String> repracticeSuggestionRefs = const <String>[],
  List<String> insufficientEvidenceReasons = const <String>[],
  int? overallScore,
}) {
  final createdAt = DateTime.utc(2026, 7, 30, 3);
  final completedAt = createdAt.add(const Duration(minutes: 2));
  final formalReview = FormalReview(
    id: id,
    practiceSessionId: 'session-$id',
    status: FormalReviewStatus.completed,
    schema: FormalReviewSchema.sceneV2,
    implementationVersion: 'qianwen-scene-review-v2',
    sourceTurnId: 'turn-$id',
    sourceTurnVersion: 'conversation-turn:evidence-v1',
    contextType: contextType,
    result: FormalReviewResult(
      eligibility: eligibility,
      overallScore: overallScore,
      summary:
          eligibility == FormalReviewSummaryEligibility.insufficientEvidence
          ? '当前回答不足以形成可靠结论。'
          : '本次回答已经形成可复盘的文本反馈。',
      dimensions: dimensions,
      feedbackItems: feedbackItems,
      repracticeSuggestionRefs: repracticeSuggestionRefs,
      insufficientEvidenceReasons: insufficientEvidenceReasons,
    ),
    createdAt: createdAt,
    updatedAt: completedAt,
    completedAt: completedAt,
  );
  return ReviewHistoryItem(
    review: presentFormalReview(formalReview),
    formalReview: formalReview,
    practiceSessionId: formalReview.practiceSessionId,
    createdAt: createdAt,
    completedAt: completedAt,
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
  final review = AgentReview(
    id: 'review-fixture-$index',
    title: title ?? '英文面试练习 · ${90 - index} 分',
    summary: summary ?? 'fixture-summary-$index',
    strength: strength ?? 'fixture-strength-$index',
    nextFocus: nextFocus ?? 'fixture-focus-$index',
  );
  return ReviewHistoryItem(
    review: review,
    formalReview: legacyFormalReviewFixture(
      review: review,
      practiceSessionId: 'session-fixture-$index',
      createdAt: created,
      completedAt: completed,
      overallScore: 90 - index,
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
  String? practiceSessionId,
  String? implementationVersion,
  String? sourceTurnId,
  String? sourceTurnVersion,
  Map<String, Object?>? result,
}) {
  return {
    'review_id': id,
    'practice_session_id': practiceSessionId ?? 'session-$score',
    'status': 'completed',
    'implementation_version': implementationVersion ?? 'review-v1',
    'source_turn_id': sourceTurnId ?? 'turn-$score',
    'source_turn_version': sourceTurnVersion ?? 'conversation-turn:evidence-v1',
    'result':
        result ??
        {
          'summary_eligibility': 'eligible',
          'overall_score': score,
          'summary': 'summary-$score',
          'conclusions': [
            {
              'key': 'clarity',
              'category': 'clarity',
              'message': 'strength-$score',
              'suggestion': 'focus-$score',
            },
          ],
        },
    'created_at': createdAt,
    'updated_at': createdAt,
    'completed_at': createdAt,
  };
}

Map<String, Object?> _wireResult(Map<String, Object?> item) {
  return item['result']! as Map<String, Object?>;
}

Map<String, Object?> _wireConclusion(Map<String, Object?> item) {
  return (_wireResult(item)['conclusions']! as List<Object?>).first
      as Map<String, Object?>;
}

Map<String, Object?> _resultAtEncodedByteSize(int targetBytes) {
  final result = <String, Object?>{
    'overall_score': 93,
    'summary': _asciiText(_testMaximumReviewTextBytes),
    'conclusions': List<Object?>.generate(4, (index) {
      return <String, Object?>{
        'key': index == 0
            ? _asciiText(_testMaximumReviewLabelBytes)
            : 'key-$index',
        'category': index == 0
            ? _asciiText(_testMaximumReviewLabelBytes)
            : 'clarity',
        'message': _asciiText(_testMaximumReviewTextBytes),
        if (index == 0) 'suggestion': 'a',
      };
    }),
  };
  final firstConclusion =
      (result['conclusions']! as List<Object?>).first as Map<String, Object?>;
  final initialBytes = utf8.encode(jsonEncode(result)).length;
  final suggestionBytes = 1 + targetBytes - initialBytes;
  if (suggestionBytes < 1 || suggestionBytes > _testMaximumReviewTextBytes) {
    throw StateError(
      'Cannot build a bounded Review Result at $targetBytes bytes.',
    );
  }
  firstConclusion['suggestion'] = _asciiText(suggestionBytes);
  if (utf8.encode(jsonEncode(result)).length != targetBytes) {
    throw StateError('Review Result byte fixture is not exact.');
  }
  return result;
}

String _utf8Text(int bytes) {
  if (bytes < 1) {
    throw ArgumentError.value(bytes, 'bytes');
  }
  final multibyteCharacters = bytes ~/ 3;
  final asciiCharacters = bytes % 3;
  return '${List<String>.filled(multibyteCharacters, '界').join()}'
      '${_asciiText(asciiCharacters)}';
}

String _asciiText(int bytes) {
  if (bytes < 0) {
    throw ArgumentError.value(bytes, 'bytes');
  }
  return List<String>.filled(bytes, 'a').join();
}

String _sourceVersionAtBytes(int bytes) {
  const prefix = 'conversation-turn:evidence-v';
  final remaining = bytes - utf8.encode(prefix).length;
  if (remaining < 1) {
    throw ArgumentError.value(bytes, 'bytes');
  }
  return '$prefix${List<String>.filled(remaining, '1').join()}';
}

String _wireReviewId(int index) {
  return '20000000-0000-4000-8000-${index.toString().padLeft(12, '0')}';
}

const _newerId = '20000000-0000-4000-8000-000000000003';
const _olderId = '20000000-0000-4000-8000-000000000002';
const _oldestId = '20000000-0000-4000-8000-000000000001';
