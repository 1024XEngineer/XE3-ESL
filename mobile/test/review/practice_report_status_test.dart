import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/evaluation/evaluation_report.dart';
import 'package:speakup/features/coaching/review/ielts_speaking_report.dart';
import 'package:speakup/features/coaching/review/ielts_practice_report_decoder.dart';
import 'package:speakup/features/coaching/review/practice_report_status.dart';
import 'package:speakup/features/coaching/review/practice_report_status_client.dart';
import 'package:speakup/features/coaching/review/practice_report_status_controller.dart';
import 'package:speakup/features/coaching/review/practice_report_status_decoder.dart';
import 'package:speakup/features/coaching/review/practice_report_status_view.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/review/wire_practice_report_status_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

void main() {
  test(
    'wire client reads the authenticated by-session status endpoint',
    () async {
      final transport = _Transport(
        IdentityHttpResponse(
          statusCode: HttpStatus.ok,
          body: jsonEncode(<String, Object?>{
            ..._baseStatus(),
            'evaluation_status': 'RUNNING',
          }),
        ),
      );
      final client = WirePracticeReportStatusClient(
        baseUri: Uri.parse('https://api.speak-up.test'),
        credentialProvider: () => const AuthSessionCredential(
          sessionToken: 'sess_practice-report',
          generation: 3,
        ),
        invalidateSession:
            ({
              required expectedSessionToken,
              required expectedGeneration,
            }) async {},
        transport: transport,
      );

      final status = await client.getStatus('session_595');

      expect(status.evaluationStatus, PracticeReportEvaluationStatus.running);
      expect(transport.method, 'GET');
      expect(
        transport.uri,
        Uri.parse(
          'https://api.speak-up.test/v1/practice-sessions/session_595/report',
        ),
      );
      expect(
        transport.headers?[HttpHeaders.authorizationHeader],
        'Bearer sess_practice-report',
      );
    },
  );

  test('wire client creates a FULL_MOCK replacement revision', () async {
    final transport = _Transport(
      const IdentityHttpResponse(statusCode: HttpStatus.accepted, body: '{}'),
    );
    final client = WirePracticeReportStatusClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_practice-report',
        generation: 3,
      ),
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      transport: transport,
    );

    await client.regenerateReport(
      _status(
        PracticeReportEvaluationStatus.failed,
        practiceMode: PracticeMode.fullMock,
        withEvaluationIdentity: true,
      ),
    );

    expect(transport.method, 'POST');
    expect(
      transport.uri,
      Uri.parse(
        'https://api.speak-up.test/v1/evaluations/'
        '7b000001-0000-4000-8000-000000000001/re-evaluate',
      ),
    );
    expect(jsonDecode(transport.body!), <String, Object>{
      'channels': <String>['SCENE'],
      'scene_strategy_ref': 'ielts-speaking-full-mock-shadow/v1',
      'pipeline_version': 'evaluation-pipeline-shadow/v1',
    });
  });

  test('default IO transport sends the FULL_MOCK regeneration body', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(() => server.close(force: true));
    final receivedBody = Completer<String>();
    server.listen((request) async {
      final body = await utf8.decoder.bind(request).join();
      if (!receivedBody.isCompleted) {
        receivedBody.complete(body);
      }
      request.response.statusCode = HttpStatus.accepted;
      request.response.write('{}');
      await request.response.close();
    });
    final client = WirePracticeReportStatusClient(
      baseUri: Uri(
        scheme: 'http',
        host: InternetAddress.loopbackIPv4.address,
        port: server.port,
      ),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_practice-report',
        generation: 3,
      ),
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
    );

    await HttpOverrides.runWithHttpOverrides<Future<void>>(
      () => client.regenerateReport(
        _status(
          PracticeReportEvaluationStatus.failed,
          practiceMode: PracticeMode.fullMock,
          withEvaluationIdentity: true,
        ),
      ),
      _RealHttpOverrides(),
    );

    expect(jsonDecode(await receivedBody.future), <String, Object>{
      'channels': <String>['SCENE'],
      'scene_strategy_ref': 'ielts-speaking-full-mock-shadow/v1',
      'pipeline_version': 'evaluation-pipeline-shadow/v1',
    });
  });

  test('wire client marks report status conflicts as retryable', () async {
    final client = WirePracticeReportStatusClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_practice-report',
        generation: 3,
      ),
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      transport: _Transport(
        const IdentityHttpResponse(statusCode: HttpStatus.conflict, body: '{}'),
      ),
    );

    await expectLater(
      client.getStatus('session_595'),
      throwsA(
        isA<PracticeReportStatusException>()
            .having(
              (error) => error.kind,
              'kind',
              PracticeReportStatusFailureKind.conflict,
            )
            .having((error) => error.retryable, 'retryable', isTrue),
      ),
    );
  });

  test('wire client rejects section regeneration before transport', () async {
    final transport = _Transport(
      const IdentityHttpResponse(statusCode: HttpStatus.accepted, body: '{}'),
    );
    final client = WirePracticeReportStatusClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => const AuthSessionCredential(
        sessionToken: 'sess_practice-report',
        generation: 3,
      ),
      invalidateSession:
          ({
            required expectedSessionToken,
            required expectedGeneration,
          }) async {},
      transport: transport,
    );

    await expectLater(
      client.regenerateReport(
        _status(
          PracticeReportEvaluationStatus.failed,
          withEvaluationIdentity: true,
        ),
      ),
      throwsA(isA<PracticeReportStatusException>()),
    );
    expect(transport.method, isNull);
  });

  test('decodes the frozen Part 2 + Part 3 READY contract', () {
    final status = decodePracticeReportStatus(<String, Object?>{
      ..._baseStatus(),
      'evaluation_status': 'READY',
      'report_ref': const <String, Object?>{
        'report_id': '20000000-0000-4000-8000-000000000002',
        'href': '/v1/evaluation-reports/20000000-0000-4000-8000-000000000002',
      },
      'scoreability_status': 'PROVISIONAL',
      'summary': '已生成 Part 2 + Part 3 专项复盘。',
    });

    expect(status.practiceMode, PracticeMode.part2);
    expect(status.reportScope, PracticeReportScope.part2And3);
    expect(status.availableSections, const <IeltsSpeakingPartId>[
      IeltsSpeakingPartId.part2,
      IeltsSpeakingPartId.part3,
    ]);
    expect(status.evaluationStatus, PracticeReportEvaluationStatus.ready);
    expect(status.evaluationId, '7b000001-0000-4000-8000-000000000001');
    expect(status.revision, 1);
  });

  test('accepts a legacy general-scene READY section report', () {
    final status = decodePracticeReportStatus(<String, Object?>{
      ..._baseStatus(),
      'detail_schema': 'general-scene-evaluation/v1',
      'evaluation_status': 'READY',
      'report_ref': const <String, Object?>{
        'report_id': '20000000-0000-4000-8000-000000000002',
        'href': '/v1/evaluation-reports/20000000-0000-4000-8000-000000000002',
      },
      'scoreability_status': 'PROVISIONAL',
      'summary': '历史专项复盘已生成。',
    });

    expect(status.detailSchema, 'general-scene-evaluation/v1');
    expect(status.evaluationStatus, PracticeReportEvaluationStatus.ready);
  });

  test('rejects legacy general-scene schema while evaluation is running', () {
    expect(
      () => decodePracticeReportStatus(<String, Object?>{
        ..._baseStatus(),
        'detail_schema': 'general-scene-evaluation/v1',
        'evaluation_status': 'RUNNING',
      }),
      throwsA(isA<PracticeReportStatusDecodeException>()),
    );
  });

  test('requires the Evaluation identity for RUNNING and READY', () {
    final missingIdentity =
        <String, Object?>{..._baseStatus(), 'evaluation_status': 'RUNNING'}
          ..remove('evaluation_id')
          ..remove('evaluation_revision_id')
          ..remove('revision');

    expect(
      () => decodePracticeReportStatus(missingIdentity),
      throwsA(isA<PracticeReportStatusDecodeException>()),
    );
  });

  test('accepts QUEUED before the Evaluation identity exists', () {
    final queued =
        <String, Object?>{..._baseStatus(), 'evaluation_status': 'QUEUED'}
          ..remove('evaluation_id')
          ..remove('evaluation_revision_id')
          ..remove('revision');

    expect(
      decodePracticeReportStatus(queued).evaluationStatus,
      PracticeReportEvaluationStatus.queued,
    );
  });

  test('rejects a report scope that contradicts practice mode', () {
    expect(
      () => decodePracticeReportStatus(<String, Object?>{
        ..._baseStatus(),
        'report_scope': 'PART_1',
        'evaluation_status': 'RUNNING',
      }),
      throwsA(isA<PracticeReportStatusDecodeException>()),
    );
  });

  test('rejects report status sections outside server order', () {
    expect(
      () => decodePracticeReportStatus(<String, Object?>{
        ..._baseStatus(),
        'available_sections': <Object?>['PART_3', 'PART_2'],
        'evaluation_status': 'RUNNING',
      }),
      throwsA(isA<PracticeReportStatusDecodeException>()),
    );
  });

  test('decodes separate Part 2 and Part 3 practice report sections', () {
    final detail = decodeIeltsPracticeReportDetail(_sectionDetail());

    expect(detail.sectionReviews, hasLength(2));
    expect(detail.sectionReviews.first.partId, IeltsSpeakingPartId.part2);
    expect(detail.sectionReviews.last.partId, IeltsSpeakingPartId.part3);
  });

  test('rejects practice report sections outside server order', () {
    final detail = _sectionDetail()
      ..['available_sections'] = <Object?>['PART_3', 'PART_2']
      ..['section_reviews'] = <Object?>[
        _sectionReview(part: 'PART_3', index: 2),
        _sectionReview(part: 'PART_2', index: 1),
      ];

    expect(
      () => decodeIeltsPracticeReportDetail(detail),
      throwsA(isA<IeltsPracticeReportDecodeException>()),
    );
  });

  test('rejects practice report questions outside server sequence', () {
    final detail = _sectionDetail()
      ..['questions'] = <Object?>[
        _sectionQuestion(part: 'PART_3', index: 2),
        _sectionQuestion(part: 'PART_2', index: 1),
      ];

    expect(
      () => decodeIeltsPracticeReportDetail(detail),
      throwsA(isA<IeltsPracticeReportDecodeException>()),
    );
  });

  test('rejects incomplete and duplicated response evidence links', () {
    final incomplete = _sectionDetail();
    final incompleteQuestion =
        (incomplete['questions']! as List<Object?>).first
            as Map<String, Object?>;
    incompleteQuestion.remove('confirmed_transcript');

    final duplicated = _sectionDetail();
    final duplicatedQuestions = duplicated['questions']! as List<Object?>;
    final duplicateQuestion = duplicatedQuestions.last as Map<String, Object?>;
    duplicateQuestion['response_turn_id'] = 'turn_1';
    duplicateQuestion['evidence_ref_ids'] = <Object?>['evidence_1'];
    final duplicatedReviews = duplicated['section_reviews']! as List<Object?>;
    (duplicatedReviews.last as Map<String, Object?>)['evidence_ref_ids'] =
        <Object?>['evidence_1'];

    for (final invalid in <Map<String, Object?>>[incomplete, duplicated]) {
      expect(
        () => decodeIeltsPracticeReportDetail(invalid),
        throwsA(isA<IeltsPracticeReportDecodeException>()),
      );
    }
  });

  test('accepts an explicitly unanswered question without evidence', () {
    final detail = _sectionDetail();
    final questions = detail['questions']! as List<Object?>;
    final unanswered = questions.last as Map<String, Object?>
      ..remove('confirmed_transcript')
      ..remove('response_turn_id')
      ..['evidence_ref_ids'] = <Object?>[];
    final reviews = detail['section_reviews']! as List<Object?>;
    (reviews.last as Map<String, Object?>)['evidence_ref_ids'] = <Object?>[];

    final decoded = decodeIeltsPracticeReportDetail(detail);

    expect(unanswered['evidence_ref_ids'], isEmpty);
    expect(decoded.questions.last.confirmedTranscript, isNull);
    expect(decoded.questions.last.responseTurnId, isNull);
  });

  test(
    'controller recovers from a retryable status failure within its bound',
    () async {
      final client = _ScriptedStatusClient(<Object>[
        const PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.conflict,
          retryable: true,
        ),
        _status(PracticeReportEvaluationStatus.ready),
      ]);
      final controller = PracticeReportStatusController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 3,
      );
      addTearDown(controller.dispose);

      await controller.load('session_595');

      expect(client.statusCalls, 2);
      expect(
        controller.status?.evaluationStatus,
        PracticeReportEvaluationStatus.ready,
      );
      expect(controller.errorMessage, isNull);
    },
  );

  test(
    'controller stops retryable status polling at the configured bound',
    () async {
      final client = _ScriptedStatusClient(const <Object>[
        PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.conflict,
          retryable: true,
        ),
      ]);
      final controller = PracticeReportStatusController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 3,
      );
      addTearDown(controller.dispose);

      await controller.load('session_595');

      expect(client.statusCalls, 3);
      expect(controller.status, isNull);
      expect(controller.errorMessage, '报告正在处理中，请稍后刷新状态。');
      expect(controller.canRetry, isTrue);
    },
  );

  test(
    'controller does not poll or offer retry for a terminal status error',
    () async {
      final client = _ScriptedStatusClient(const <Object>[
        PracticeReportStatusException(
          kind: PracticeReportStatusFailureKind.authenticationRequired,
        ),
      ]);
      final controller = PracticeReportStatusController(
        client: client,
        pollInterval: Duration.zero,
        maximumPollAttempts: 3,
      );
      addTearDown(controller.dispose);

      await controller.load('session_595');

      expect(client.statusCalls, 1);
      expect(controller.canRetry, isFalse);
    },
  );

  testWidgets('renders real running, ready, insufficient, and failed states', (
    tester,
  ) async {
    final client = _StatusClient();
    final controller = PracticeReportStatusController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 1,
    );
    addTearDown(controller.dispose);
    var opened = false;

    Future<void> pumpCard() => tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: PracticeReportStatusCard(
              controller: controller,
              onOpenReport: () async => opened = true,
            ),
          ),
        ),
      ),
    );

    client.status = _status(PracticeReportEvaluationStatus.running);
    await controller.load('session_595');
    await pumpCard();
    expect(
      find.byKey(const Key('ielts-completion-report-generating')),
      findsOneWidget,
    );
    expect(find.byType(CircularProgressIndicator), findsNothing);

    client.status = _status(PracticeReportEvaluationStatus.ready);
    await controller.retry();
    await tester.pump();
    expect(
      find.byKey(const Key('ielts-completion-report-ready')),
      findsOneWidget,
    );
    expect(find.text('复盘已生成'), findsOneWidget);

    client.status = _status(
      PracticeReportEvaluationStatus.ready,
      scoreability: EvaluationReportScoreability.insufficient,
    );
    await controller.retry();
    await tester.pump();
    expect(
      find.byKey(const Key('ielts-completion-report-ready')),
      findsOneWidget,
    );
    expect(find.text('复盘已生成 · 证据不足'), findsOneWidget);
    await tester.tap(find.byKey(const Key('ielts-completion-report-open')));
    await tester.pump();
    expect(opened, isTrue);

    client.status = _status(PracticeReportEvaluationStatus.failed);
    await controller.retry();
    await tester.pump();
    expect(
      find.byKey(const Key('ielts-completion-report-failed')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('ielts-completion-report-retry')),
      findsNothing,
    );
    expect(find.textContaining('EVALUATION_FAILED'), findsNothing);
    expect(find.textContaining('回答已保存'), findsOneWidget);
  });

  testWidgets('retryable FULL_MOCK FAILED creates a new revision', (
    tester,
  ) async {
    final client = _RegeneratingStatusClient(
      _status(
        PracticeReportEvaluationStatus.failed,
        practiceMode: PracticeMode.fullMock,
        withEvaluationIdentity: true,
      ),
    );
    final controller = PracticeReportStatusController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 1,
    );
    addTearDown(controller.dispose);
    await controller.load('session_595');
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PracticeReportStatusCard(
            controller: controller,
            onOpenReport: () async {},
          ),
        ),
      ),
    );

    expect(
      find.byKey(const Key('ielts-completion-report-retry')),
      findsOneWidget,
    );
    await tester.tap(find.byKey(const Key('ielts-completion-report-retry')));
    await tester.pumpAndSettle();

    expect(client.regenerationCalls, 1);
    expect(client.statusCalls, 2);
    expect(
      find.byKey(const Key('ielts-completion-report-generating')),
      findsOneWidget,
    );
  });

  testWidgets('READY report loading failure stays visible and can be retried', (
    tester,
  ) async {
    final client = _StatusClient()
      ..status = _status(PracticeReportEvaluationStatus.ready)
      ..readyReportError = const PracticeReportStatusException(
        kind: PracticeReportStatusFailureKind.network,
        retryable: true,
      );
    final controller = PracticeReportStatusController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 1,
    );
    addTearDown(controller.dispose);
    await controller.load('session_595');
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PracticeReportStatusCard(
            controller: controller,
            onOpenReport: () async {
              await controller.loadReadyReport();
            },
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('ielts-completion-report-open')));
    await tester.pumpAndSettle();

    expect(client.readyReportCalls, 1);
    expect(find.text('报告暂时无法加载，请稍后重试。'), findsOneWidget);
    expect(find.text('重试打开'), findsOneWidget);
  });

  testWidgets('section FAILED with identity cannot regenerate', (tester) async {
    final client = _RegeneratingStatusClient(
      _status(
        PracticeReportEvaluationStatus.failed,
        withEvaluationIdentity: true,
      ),
    );
    final controller = PracticeReportStatusController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 1,
    );
    addTearDown(controller.dispose);
    await controller.load('session_595');
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PracticeReportStatusCard(
            controller: controller,
            onOpenReport: () async {},
          ),
        ),
      ),
    );

    expect(
      find.byKey(const Key('ielts-completion-report-retry')),
      findsNothing,
    );
    await controller.regenerate();
    expect(client.regenerationCalls, 0);
    expect(client.statusCalls, 1);
  });

  testWidgets('handoff FAILED without identity cannot regenerate', (
    tester,
  ) async {
    final client = _RegeneratingStatusClient(
      _status(PracticeReportEvaluationStatus.failed),
    );
    final controller = PracticeReportStatusController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 1,
    );
    addTearDown(controller.dispose);
    await controller.load('session_595');
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PracticeReportStatusCard(
            controller: controller,
            onOpenReport: () async {},
          ),
        ),
      ),
    );

    expect(
      find.byKey(const Key('ielts-completion-report-retry')),
      findsNothing,
    );
    await controller.regenerate();
    expect(client.regenerationCalls, 0);
    expect(client.statusCalls, 1);
  });

  testWidgets('status card fits narrow screens with large text', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final client = _StatusClient()
      ..status = _status(PracticeReportEvaluationStatus.ready);
    final controller = PracticeReportStatusController(
      client: client,
      pollInterval: Duration.zero,
      maximumPollAttempts: 1,
    );
    addTearDown(controller.dispose);
    await controller.load('session_595');

    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(textScaler: TextScaler.linear(2)),
          child: Scaffold(
            body: SingleChildScrollView(
              padding: const EdgeInsets.all(12),
              child: PracticeReportStatusCard(
                controller: controller,
                onOpenReport: () async {},
              ),
            ),
          ),
        ),
      ),
    );
    expect(tester.takeException(), isNull);
  });
}

Map<String, Object?> _baseStatus() => <String, Object?>{
  'practice_session_id': 'session_595',
  'practice_mode': 'PART_2',
  'report_scope': 'PART_2_3',
  'available_sections': <Object?>['PART_2', 'PART_3'],
  'detail_schema': 'ielts-speaking-practice-report/v1',
  'evaluation_id': '7b000001-0000-4000-8000-000000000001',
  'evaluation_revision_id': 'a1000001-0000-4000-8000-000000000001',
  'revision': 1,
  'status_url': '/v1/practice-sessions/session_595/report',
};

Map<String, Object?> _sectionDetail() => <String, Object?>{
  'schema_version': 'ielts-speaking-practice-report/v1',
  'report_scope': 'PART_2_3',
  'available_sections': <Object?>['PART_2', 'PART_3'],
  'questions': <Object?>[
    _sectionQuestion(part: 'PART_2', index: 1),
    _sectionQuestion(part: 'PART_3', index: 2),
  ],
  'section_reviews': <Object?>[
    _sectionReview(part: 'PART_2', index: 1),
    _sectionReview(part: 'PART_3', index: 2),
  ],
};

Map<String, Object?> _sectionQuestion({
  required String part,
  required int index,
}) => <String, Object?>{
  'question_id': 'question_$index',
  'part_id': part,
  'index': index,
  'question_text': 'Question $index?',
  'confirmed_transcript': 'Answer $index.',
  'response_turn_id': 'turn_$index',
  'evidence_ref_ids': <Object?>['evidence_$index'],
};

Map<String, Object?> _sectionReview({
  required String part,
  required int index,
}) => <String, Object?>{
  'part_id': part,
  'question_indexes': <Object?>[index],
  'evidence_ref_ids': <Object?>['evidence_$index'],
  'strength_finding_ids': <Object?>[],
  'improvement_finding_ids': <Object?>[],
  'upgrade_example_finding_ids': <Object?>[],
};

PracticeReportStatus _status(
  PracticeReportEvaluationStatus evaluationStatus, {
  EvaluationReportScoreability scoreability =
      EvaluationReportScoreability.provisional,
  PracticeMode practiceMode = PracticeMode.part2,
  bool withEvaluationIdentity = false,
}) {
  final ready = evaluationStatus == PracticeReportEvaluationStatus.ready;
  final failed = evaluationStatus == PracticeReportEvaluationStatus.failed;
  final hasEvaluation = ready || withEvaluationIdentity;
  final reportScope = switch (practiceMode) {
    PracticeMode.part1 => PracticeReportScope.part1,
    PracticeMode.part2 => PracticeReportScope.part2And3,
    PracticeMode.part3 => PracticeReportScope.part3,
    PracticeMode.fullMock => PracticeReportScope.fullMock,
    _ => throw StateError('Unsupported test mode.'),
  };
  final sections = switch (practiceMode) {
    PracticeMode.part1 => const <IeltsSpeakingPartId>[
      IeltsSpeakingPartId.part1,
    ],
    PracticeMode.part2 => const <IeltsSpeakingPartId>[
      IeltsSpeakingPartId.part2,
      IeltsSpeakingPartId.part3,
    ],
    PracticeMode.part3 => const <IeltsSpeakingPartId>[
      IeltsSpeakingPartId.part3,
    ],
    PracticeMode.fullMock => const <IeltsSpeakingPartId>[
      IeltsSpeakingPartId.part1,
      IeltsSpeakingPartId.part2,
      IeltsSpeakingPartId.part3,
    ],
    _ => throw StateError('Unsupported test mode.'),
  };
  return PracticeReportStatus(
    practiceSessionId: 'session_595',
    practiceMode: practiceMode,
    reportScope: reportScope,
    availableSections: sections,
    detailSchema: practiceMode == PracticeMode.fullMock
        ? 'ielts-speaking-report/v1'
        : 'ielts-speaking-practice-report/v1',
    evaluationStatus: evaluationStatus,
    statusUrl: '/v1/practice-sessions/session_595/report',
    evaluationId: hasEvaluation ? '7b000001-0000-4000-8000-000000000001' : null,
    evaluationRevisionId: hasEvaluation
        ? 'a1000001-0000-4000-8000-000000000001'
        : null,
    revision: hasEvaluation ? 1 : null,
    reportRef: ready
        ? const PracticeReportRef(
            reportId: '20000000-0000-4000-8000-000000000002',
            href: '/v1/evaluation-reports/20000000-0000-4000-8000-000000000002',
          )
        : null,
    scoreability: ready ? scoreability : null,
    summary: ready ? '复盘已生成。' : null,
    stableFailure: failed
        ? const PracticeReportStableFailure(
            reasonCode: 'EVALUATION_FAILED',
            retryable: true,
          )
        : null,
  );
}

final class _StatusClient implements PracticeReportStatusClient {
  PracticeReportStatus status = _status(PracticeReportEvaluationStatus.running);
  Object? readyReportError;
  int readyReportCalls = 0;

  @override
  Future<PracticeReportStatus> getStatus(String practiceSessionId) async =>
      status;

  @override
  Future<EvaluationReport> getReadyReport(PracticeReportRef reportRef) {
    readyReportCalls++;
    final error = readyReportError;
    if (error != null) {
      return Future<EvaluationReport>.error(error);
    }
    throw UnimplementedError();
  }

  @override
  Future<void> clearAccountState() async {}
}

final class _ScriptedStatusClient implements PracticeReportStatusClient {
  _ScriptedStatusClient(this.results);

  final List<Object> results;
  int statusCalls = 0;

  @override
  Future<PracticeReportStatus> getStatus(String practiceSessionId) async {
    final index = statusCalls.clamp(0, results.length - 1);
    statusCalls++;
    final result = results[index];
    if (result is PracticeReportStatusException) {
      throw result;
    }
    return result as PracticeReportStatus;
  }

  @override
  Future<EvaluationReport> getReadyReport(PracticeReportRef reportRef) =>
      throw UnimplementedError();

  @override
  Future<void> clearAccountState() async {}
}

final class _RegeneratingStatusClient
    implements PracticeReportStatusClient, PracticeReportRegenerationClient {
  _RegeneratingStatusClient(this.status);

  PracticeReportStatus status;
  int statusCalls = 0;
  int regenerationCalls = 0;

  @override
  Future<PracticeReportStatus> getStatus(String practiceSessionId) async {
    statusCalls++;
    return status;
  }

  @override
  Future<void> regenerateReport(PracticeReportStatus status) async {
    regenerationCalls++;
    this.status = _status(
      PracticeReportEvaluationStatus.running,
      practiceMode: status.practiceMode,
      withEvaluationIdentity: true,
    );
  }

  @override
  Future<EvaluationReport> getReadyReport(PracticeReportRef reportRef) =>
      throw UnimplementedError();

  @override
  Future<void> clearAccountState() async {}
}

final class _Transport implements IdentityHttpTransport {
  _Transport(this.response);

  final IdentityHttpResponse response;
  String? method;
  Uri? uri;
  Map<String, String>? headers;
  String? body;

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    this.method = method;
    this.uri = uri;
    this.headers = headers;
    this.body = body;
    return response;
  }
}

final class _RealHttpOverrides extends HttpOverrides {}
