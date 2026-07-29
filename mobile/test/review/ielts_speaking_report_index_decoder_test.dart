import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/review/ielts_speaking_report_index.dart';
import 'package:speakup/review/ielts_speaking_report_index_decoder.dart';

import 'ielts_speaking_report_fixture.dart';

void main() {
  test('decodes an explicit IELTS report history page', () {
    final page = decodeIeltsSpeakingReportIndex(
      ieltsSpeakingReportContractFixture()['index_page'],
    );

    expect(page.items, hasLength(1));
    expect(page.items.single.practiceSessionId, 'session_ielts_report_001');
    expect(page.items.single.reportKind, IeltsSpeakingReportKind.fullMock);
    expect(page.nextCursor, 'eyJpZCI6ImlsdHNfMDAxIn0');
  });

  test('rejects unknown report kinds and a mismatched resource URL', () {
    final unknownKind = cloneIeltsSpeakingReportFixture(
      ieltsSpeakingReportContractFixture()['index_page'],
    );
    _first(unknownKind)['report_kind'] = 'INTERVIEW';
    expect(
      () => decodeIeltsSpeakingReportIndex(unknownKind),
      throwsA(isA<IeltsSpeakingReportIndexDecodeException>()),
    );

    final mismatchedUrl = cloneIeltsSpeakingReportFixture(
      ieltsSpeakingReportContractFixture()['index_page'],
    );
    _first(mismatchedUrl)['status_url'] =
        '/v1/practice-sessions/session_other/ielts-speaking-report';
    expect(
      () => decodeIeltsSpeakingReportIndex(mismatchedUrl),
      throwsA(isA<IeltsSpeakingReportIndexDecodeException>()),
    );
  });

  test('rejects duplicate logical reports and reversed timestamps', () {
    final duplicate = cloneIeltsSpeakingReportFixture(
      ieltsSpeakingReportContractFixture()['index_page'],
    );
    final items = duplicate['items']! as List<Object?>;
    items.add(cloneIeltsSpeakingReportFixture(items.single));
    expect(
      () => decodeIeltsSpeakingReportIndex(duplicate),
      throwsA(isA<IeltsSpeakingReportIndexDecodeException>()),
    );

    final reversed = cloneIeltsSpeakingReportFixture(
      ieltsSpeakingReportContractFixture()['index_page'],
    );
    _first(reversed)['updated_at'] = '2026-07-30T07:59:59Z';
    expect(
      () => decodeIeltsSpeakingReportIndex(reversed),
      throwsA(isA<IeltsSpeakingReportIndexDecodeException>()),
    );

    final invalidCalendarDate = cloneIeltsSpeakingReportFixture(
      ieltsSpeakingReportContractFixture()['index_page'],
    );
    _first(invalidCalendarDate)['created_at'] = '2026-02-31T08:00:00Z';
    expect(
      () => decodeIeltsSpeakingReportIndex(invalidCalendarDate),
      throwsA(isA<IeltsSpeakingReportIndexDecodeException>()),
    );
  });
}

Map<String, Object?> _first(Map<String, Object?> root) =>
    (root['items']! as List<Object?>).single as Map<String, Object?>;
