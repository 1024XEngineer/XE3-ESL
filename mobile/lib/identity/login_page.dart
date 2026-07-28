import 'package:flutter/material.dart';
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
              const SizedBox(height: 14),
              AuthPasswordField(
                controller: _passwordController,
                obscureText: _obscurePassword,
                minimumLength: 15,
                helperText: '15–128 个字符',
                onToggleVisibility: () =>
                    setState(() => _obscurePassword = !_obscurePassword),
                onSubmitted: (_) => _submit(),
              ),
              const SizedBox(height: 28),
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
            final minHeight = constraints.maxHeight > 48
                ? constraints.maxHeight - 48
                : 0.0;
            return SingleChildScrollView(
              keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
              padding: const EdgeInsets.fromLTRB(24, 30, 24, 18),
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
                        alignment: Alignment.center,
                        child: AuthWordmark(),
                      ),
                      const SizedBox(height: 38),
                      Text(
                        title,
                        textAlign: TextAlign.center,
                        style: const TextStyle(
                          color: AuthPalette.ink,
                          fontSize: 27,
                          height: 1.1,
                          letterSpacing: -0.5,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                      const SizedBox(height: 12),
                      Text(
                        subtitle,
                        textAlign: TextAlign.center,
                        style: const TextStyle(
                          color: AuthPalette.muted,
                          fontSize: 15,
                          height: 1.5,
                        ),
                      ),
                      const SizedBox(height: 6),
                      AuthSwitchPrompt(
                        prompt: switchPrompt,
                        actionLabel: switchActionLabel,
                        onPressed: onSwitch,
                      ),
                      if (message != null) ...[
                        const SizedBox(height: 18),
                        AuthMessage(message: message!),
                      ],
                      if (errorMessage != null) ...[
                        const SizedBox(height: 18),
                        AuthMessage(message: errorMessage!, isError: true),
                      ],
                      const SizedBox(height: 44),
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
      textAlign: TextAlign.center,
      style: const TextStyle(color: AuthPalette.muted, fontSize: 14),
    );
    final action = TextButton(
      onPressed: onPressed,
      style: TextButton.styleFrom(
        foregroundColor: AuthPalette.link,
        padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 7),
        minimumSize: Size.zero,
        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
        textStyle: const TextStyle(
          fontSize: 14,
          fontWeight: FontWeight.w600,
          decoration: TextDecoration.underline,
          decorationThickness: 1,
        ),
      ),
      child: Text(actionLabel),
    );
    if (MediaQuery.textScalerOf(context).scale(1) > 1.6) {
      return Column(
        mainAxisSize: MainAxisSize.min,
        children: [promptText, action],
      );
    }
    return Wrap(
      alignment: WrapAlignment.center,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [promptText, action],
    );
  }
}

abstract final class AuthPalette {
  static const background = Color(0xFFFFFBF6);
  static const field = Color(0xFFFFFFFF);
  static const ink = Color(0xFF202628);
  static const muted = Color(0xFF666D70);
  static const border = Color(0xFFD9D6D1);
  static const focusedBorder = Color(0xFF516C74);
  static const primary = Color(0xFF183F49);
  static const disabled = Color(0xFFD8D6D2);
  static const link = Color(0xFF246B87);
  static const noticeBackground = Color(0xFFEAF3EF);
  static const noticeForeground = Color(0xFF285443);
  static const errorBackground = Color(0xFFFFEDE8);
  static const errorForeground = Color(0xFF8A2D21);
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
  const borderRadius = BorderRadius.all(Radius.circular(11));
  return InputDecoration(
    hintText: label,
    hintStyle: const TextStyle(color: AuthPalette.muted, fontSize: 16),
    helperText: helperText,
    helperStyle: const TextStyle(
      color: AuthPalette.muted,
      fontSize: 12,
      height: 1.3,
    ),
    filled: true,
    fillColor: AuthPalette.field,
    contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 18),
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
      style: FilledButton.styleFrom(
        backgroundColor: AuthPalette.primary,
        foregroundColor: Colors.white,
        disabledBackgroundColor: AuthPalette.disabled,
        disabledForegroundColor: AuthPalette.muted,
        minimumSize: const Size.fromHeight(54),
        shape: const StadiumBorder(),
        elevation: 0,
        textStyle: const TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
      ),
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
        if (length < minimumLength || length > 128) {
          return '密码长度需为 $minimumLength–128 个字符。';
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
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        decoration: BoxDecoration(
          color: isError
              ? AuthPalette.errorBackground
              : AuthPalette.noticeBackground,
          borderRadius: BorderRadius.circular(10),
        ),
        child: Text(
          message,
          style: TextStyle(
            color: isError
                ? AuthPalette.errorForeground
                : AuthPalette.noticeForeground,
            fontSize: 14,
            height: 1.4,
          ),
        ),
      ),
    );
  }
}
