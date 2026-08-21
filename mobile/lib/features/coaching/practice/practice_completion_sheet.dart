import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';

/// Blocks the active practice surface and presents the available actions after
/// the server has confirmed that the current practice session is complete.
class PracticeCompletionOverlay extends StatelessWidget {
  const PracticeCompletionOverlay({
    required this.title,
    required this.message,
    required this.primaryLabel,
    required this.secondaryLabel,
    required this.onPrimary,
    required this.onSecondary,
    this.primaryLoading = false,
    this.keyPrefix = 'practice-completion',
    this.sheetKey,
    this.primaryKey,
    this.secondaryKey,
    super.key,
  });

  final String title;
  final String message;
  final String primaryLabel;
  final String secondaryLabel;
  final VoidCallback? onPrimary;
  final VoidCallback? onSecondary;
  final bool primaryLoading;
  final String keyPrefix;
  final Key? sheetKey;
  final Key? primaryKey;
  final Key? secondaryKey;

  @override
  Widget build(BuildContext context) {
    return Stack(
      key: Key('$keyPrefix-overlay'),
      fit: StackFit.expand,
      children: [
        const ModalBarrier(
          dismissible: false,
          color: Color(0x14000000),
          semanticsLabel: '练习完成',
        ),
        Align(
          alignment: Alignment.bottomCenter,
          child: SafeArea(
            top: false,
            child: SingleChildScrollView(
              reverse: true,
              padding: const EdgeInsets.fromLTRB(10, 0, 10, 8),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 560),
                child: Material(
                  key: sheetKey ?? Key('$keyPrefix-sheet'),
                  color: SpeakUpDesign.surface,
                  elevation: 12,
                  shadowColor: const Color(0x26000000),
                  borderRadius: BorderRadius.circular(28),
                  clipBehavior: Clip.antiAlias,
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(20, 12, 20, 16),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Center(
                          child: Container(
                            width: 42,
                            height: 4,
                            decoration: BoxDecoration(
                              color: SpeakUpDesign.border,
                              borderRadius: BorderRadius.circular(99),
                            ),
                          ),
                        ),
                        const SizedBox(height: 18),
                        Center(
                          child: Container(
                            width: 52,
                            height: 52,
                            decoration: BoxDecoration(
                              shape: BoxShape.circle,
                              border: Border.all(
                                color: SpeakUpDesign.ink,
                                width: 2,
                              ),
                            ),
                            child: const Icon(
                              Icons.check_rounded,
                              size: 34,
                              color: SpeakUpDesign.ink,
                            ),
                          ),
                        ),
                        const SizedBox(height: 16),
                        Text(
                          title,
                          textAlign: TextAlign.center,
                          style: SpeakUpDesign.pageTitle.copyWith(fontSize: 24),
                        ),
                        const SizedBox(height: 6),
                        Text(
                          message,
                          textAlign: TextAlign.center,
                          style: SpeakUpDesign.body.copyWith(
                            color: SpeakUpDesign.secondary,
                          ),
                        ),
                        const SizedBox(height: 20),
                        FilledButton(
                          key: primaryKey ?? Key('$keyPrefix-primary'),
                          onPressed: primaryLoading ? null : onPrimary,
                          style: FilledButton.styleFrom(
                            minimumSize: const Size.fromHeight(54),
                            backgroundColor: SpeakUpDesign.ink,
                            foregroundColor: Colors.white,
                            shape: RoundedRectangleBorder(
                              borderRadius: BorderRadius.circular(14),
                            ),
                          ),
                          child: primaryLoading
                              ? const SizedBox.square(
                                  dimension: 20,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                    color: Colors.white,
                                  ),
                                )
                              : Text(primaryLabel),
                        ),
                        const SizedBox(height: 4),
                        TextButton(
                          key: secondaryKey ?? Key('$keyPrefix-secondary'),
                          onPressed: onSecondary,
                          style: TextButton.styleFrom(
                            foregroundColor: SpeakUpDesign.ink,
                            minimumSize: const Size.fromHeight(44),
                          ),
                          child: Text(secondaryLabel),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }
}
