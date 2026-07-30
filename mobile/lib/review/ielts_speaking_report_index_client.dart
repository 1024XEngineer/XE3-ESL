import 'package:speakup/review/ielts_speaking_report_index.dart';

abstract interface class IeltsSpeakingReportIndexClient {
  Future<IeltsSpeakingReportIndexPage> listReports({
    String? cursor,
    int limit = 20,
  });

  Future<void> clearAccountState();
}
