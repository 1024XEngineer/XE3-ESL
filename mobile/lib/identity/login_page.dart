import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/auth_input.dart';
import 'package:speakup/identity/auth_state.dart';

class LoginPage extends StatefulWidget {
  const LoginPage({required this.controller, required this.state, super.key});

  final AuthController controller;
  final AuthSignedOut state;

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  final _formKey = GlobalKey<FormState>();
  final _emailController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _obscurePassword = true;

  @override
  void dispose() {
    _emailController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AuthFormScaffold(
      title: '欢迎回来',
      subtitle: '继续你的英语表达练习。',
      switchPrompt: '第一次使用 SpeakUp？',
      switchActionLabel: '创建账号',
      onSwitch: widget.state.isSubmitting
          ? null
          : widget.controller.showRegister,
      message: widget.state.noticeMessage,
      errorMessage: widget.state.errorMessage,
      child: AutofillGroup(
        child: Form(
          key: _formKey,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              AuthEmailField(controller: _emailController),
              const SizedBox(height: SpeakUpDesign.space16),
              AuthPasswordField(
                controller: _passwordController,
                obscureText: _obscurePassword,
                minimumLength: 1,
                onToggleVisibility: () =>
                    setState(() => _obscurePassword = !_obscurePassword),
                onSubmitted: (_) => _submit(),
              ),
              const SizedBox(height: SpeakUpDesign.space24),
              AuthPrimaryButton(
                label: '登录',
                isSubmitting: widget.state.isSubmitting,
                onPressed: widget.state.isSubmitting ? null : _submit,
              ),
            ],
          ),
        ),
      ),
    );
  }

  void _submit() {
    FocusManager.instance.primaryFocus?.unfocus();
    if (!(_formKey.currentState?.validate() ?? false)) {
      return;
    }
    widget.controller.login(
      email: normalizeIdentityEmailInput(_emailController.text),
      password: _passwordController.text,
    );
  }
}

class AuthFormScaffold extends StatelessWidget {
  const AuthFormScaffold({
    required this.title,
    required this.subtitle,
    required this.switchPrompt,
    required this.switchActionLabel,
    required this.onSwitch,
    required this.child,
    this.message,
    this.errorMessage,
    super.key,
  });

  final String title;
  final String subtitle;
  final String switchPrompt;
  final String switchActionLabel;
  final VoidCallback? onSwitch;
  final String? message;
  final String? errorMessage;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: AuthPalette.background,
      body: SafeArea(
        child: LayoutBuilder(
          builder: (context, constraints) {
            final horizontal = SpeakUpDesign.horizontalInset(context);
            final minHeight = constraints.maxHeight > 40
                ? constraints.maxHeight - 40
                : 0.0;
            return SingleChildScrollView(
              keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
              padding: EdgeInsets.fromLTRB(
                horizontal,
                SpeakUpDesign.space20,
                horizontal,
                SpeakUpDesign.space20,
              ),
              child: Center(
                child: ConstrainedBox(
                  constraints: BoxConstraints(
                    minHeight: minHeight,
                    maxWidth: 440,
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      const Align(
                        alignment: Alignment.centerLeft,
                        child: AuthWordmark(),
                      ),
                      const SizedBox(height: SpeakUpDesign.space32),
                      Text(
                        title,
                        style: Theme.of(context).textTheme.headlineLarge,
                      ),
                      const SizedBox(height: SpeakUpDesign.space8),
                      Text(
                        subtitle,
                        style: Theme.of(context).textTheme.bodyMedium,
                      ),
                      const SizedBox(height: SpeakUpDesign.space4),
                      AuthSwitchPrompt(
                        prompt: switchPrompt,
                        actionLabel: switchActionLabel,
                        onPressed: onSwitch,
                      ),
                      if (message != null) ...[
                        const SizedBox(height: SpeakUpDesign.space16),
                        AuthMessage(message: message!),
                      ],
                      if (errorMessage != null) ...[
                        const SizedBox(height: SpeakUpDesign.space16),
                        AuthMessage(message: errorMessage!, isError: true),
                      ],
                      const SizedBox(height: SpeakUpDesign.space32),
                      child,
                    ],
                  ),
                ),
              ),
            );
          },
        ),
      ),
    );
  }
}

class AuthSwitchPrompt extends StatelessWidget {
  const AuthSwitchPrompt({
    required this.prompt,
    required this.actionLabel,
    required this.onPressed,
    super.key,
  });

  final String prompt;
  final String actionLabel;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    final promptText = Text(
      prompt,
      style: Theme.of(context).textTheme.bodyMedium,
    );
    final action = TextButton(
      onPressed: onPressed,
      style: TextButton.styleFrom(
        padding: const EdgeInsets.symmetric(horizontal: 6),
      ),
      child: Text(actionLabel),
    );
    if (MediaQuery.textScalerOf(context).scale(1) > 1.6) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [promptText, action],
      );
    }
    return Wrap(
      alignment: WrapAlignment.start,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [promptText, action],
    );
  }
}

abstract final class AuthPalette {
  static const background = SpeakUpDesign.canvas;
  static const field = SpeakUpDesign.surface;
  static const ink = SpeakUpDesign.ink;
  static const muted = SpeakUpDesign.secondary;
  static const border = SpeakUpDesign.border;
  static const focusedBorder = SpeakUpDesign.primary;
  static const primary = SpeakUpDesign.primary;
  static const disabled = SpeakUpDesign.surfaceMuted;
  static const link = SpeakUpDesign.primary;
  static const noticeBackground = SpeakUpDesign.successMuted;
  static const noticeForeground = SpeakUpDesign.success;
  static const errorBackground = SpeakUpDesign.errorMuted;
  static const errorForeground = SpeakUpDesign.error;
}

class AuthWordmark extends StatelessWidget {
  const AuthWordmark({super.key});

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: 'SpeakUp',
      image: true,
      child: MediaQuery.withClampedTextScaling(
        maxScaleFactor: 1.5,
        child: ExcludeSemantics(
          child: SizedBox(
            height: 30,
            child: Stack(
              clipBehavior: Clip.none,
              children: [
                const Text.rich(
                  TextSpan(
                    children: [
                      TextSpan(text: 'speak'),
                      TextSpan(
                        text: 'up',
                        style: TextStyle(
                          color: AuthPalette.primary,
                          fontStyle: FontStyle.italic,
                        ),
                      ),
                    ],
                  ),
                  style: TextStyle(
                    color: AuthPalette.ink,
                    fontSize: 23,
                    height: 1,
                    letterSpacing: -1.2,
                    fontWeight: FontWeight.w800,
                  ),
                ),
                Positioned(
                  right: 1,
                  bottom: 0,
                  child: Transform.rotate(
                    angle: -0.08,
                    child: Container(
                      width: 28,
                      height: 2.5,
                      decoration: BoxDecoration(
                        color: AuthPalette.primary,
                        borderRadius: BorderRadius.circular(2),
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

InputDecoration authFieldDecoration({
  required String label,
  String? helperText,
  Widget? suffixIcon,
}) {
  const borderRadius = BorderRadius.all(
    Radius.circular(SpeakUpDesign.radiusControl),
  );
  return InputDecoration(
    hintText: label,
    hintStyle: SpeakUpDesign.body,
    helperText: helperText,
    helperStyle: SpeakUpDesign.meta,
    filled: true,
    fillColor: AuthPalette.field,
    contentPadding: const EdgeInsets.symmetric(
      horizontal: SpeakUpDesign.space16,
      vertical: SpeakUpDesign.space16,
    ),
    suffixIcon: suffixIcon,
    border: const OutlineInputBorder(
      borderRadius: borderRadius,
      borderSide: BorderSide(color: AuthPalette.border),
    ),
    enabledBorder: const OutlineInputBorder(
      borderRadius: borderRadius,
      borderSide: BorderSide(color: AuthPalette.border),
    ),
    focusedBorder: const OutlineInputBorder(
      borderRadius: borderRadius,
      borderSide: BorderSide(color: AuthPalette.focusedBorder, width: 1.5),
    ),
    errorBorder: const OutlineInputBorder(
      borderRadius: borderRadius,
      borderSide: BorderSide(color: AuthPalette.errorForeground),
    ),
    focusedErrorBorder: const OutlineInputBorder(
      borderRadius: borderRadius,
      borderSide: BorderSide(color: AuthPalette.errorForeground, width: 1.5),
    ),
  );
}

class AuthPrimaryButton extends StatelessWidget {
  const AuthPrimaryButton({
    required this.label,
    required this.isSubmitting,
    required this.onPressed,
    super.key,
  });

  final String label;
  final bool isSubmitting;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return FilledButton(
      onPressed: onPressed,
      style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(52)),
      child: isSubmitting ? const AuthButtonProgress() : Text(label),
    );
  }
}

class AuthEmailField extends StatelessWidget {
  const AuthEmailField({required this.controller, super.key});

  final TextEditingController controller;

  @override
  Widget build(BuildContext context) {
    return TextFormField(
      controller: controller,
      keyboardType: TextInputType.emailAddress,
      autofillHints: const [AutofillHints.email],
      autocorrect: false,
      textInputAction: TextInputAction.next,
      onTapOutside: (_) => FocusManager.instance.primaryFocus?.unfocus(),
      decoration: authFieldDecoration(label: '邮箱'),
      validator: (value) {
        if (!isValidIdentityEmailInput(value ?? '')) {
          return '请输入有效的邮箱地址。';
        }
        return null;
      },
    );
  }
}

class AuthPasswordField extends StatelessWidget {
  const AuthPasswordField({
    required this.controller,
    required this.obscureText,
    required this.minimumLength,
    required this.onToggleVisibility,
    this.autofillHint = AutofillHints.password,
    this.helperText,
    this.onSubmitted,
    super.key,
  });

  final TextEditingController controller;
  final bool obscureText;
  final int minimumLength;
  final VoidCallback onToggleVisibility;
  final String autofillHint;
  final String? helperText;
  final ValueChanged<String>? onSubmitted;

  @override
  Widget build(BuildContext context) {
    return TextFormField(
      controller: controller,
      obscureText: obscureText,
      autofillHints: [autofillHint],
      enableSuggestions: false,
      autocorrect: false,
      textInputAction: TextInputAction.done,
      onFieldSubmitted: onSubmitted,
      onTapOutside: (_) => FocusManager.instance.primaryFocus?.unfocus(),
      decoration: authFieldDecoration(
        label: '密码',
        helperText: helperText,
        suffixIcon: IconButton(
          onPressed: onToggleVisibility,
          tooltip: obscureText ? '显示密码' : '隐藏密码',
          color: AuthPalette.muted,
          icon: Icon(
            obscureText
                ? Icons.visibility_outlined
                : Icons.visibility_off_outlined,
          ),
        ),
      ),
      validator: (value) {
        final length = value?.runes.length ?? 0;
        if (length == 0) {
          return '请输入密码。';
        }
        if (length < minimumLength) {
          return '密码至少需要 $minimumLength 个字符。';
        }
        if (length > 128) {
          return '密码不能超过 128 个字符。';
        }
        return null;
      },
    );
  }
}

class AuthButtonProgress extends StatelessWidget {
  const AuthButtonProgress({super.key});

  @override
  Widget build(BuildContext context) {
    return const SizedBox.square(
      dimension: 20,
      child: CircularProgressIndicator(color: Colors.white, strokeWidth: 2),
    );
  }
}

class AuthMessage extends StatelessWidget {
  const AuthMessage({required this.message, this.isError = false, super.key});

  final String message;
  final bool isError;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      liveRegion: true,
      child: Container(
        padding: const EdgeInsets.symmetric(
          horizontal: SpeakUpDesign.space16,
          vertical: SpeakUpDesign.space12,
        ),
        decoration: BoxDecoration(
          color: isError
              ? AuthPalette.errorBackground
              : AuthPalette.noticeBackground,
          borderRadius: BorderRadius.circular(SpeakUpDesign.radiusControl),
        ),
        child: Text(
          message,
          style: TextStyle(
            color: isError
                ? AuthPalette.errorForeground
                : AuthPalette.noticeForeground,
            fontSize: SpeakUpDesign.body.fontSize,
            height: SpeakUpDesign.body.height,
          ),
        ),
      ),
    );
  }
}
