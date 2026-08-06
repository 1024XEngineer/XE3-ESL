import 'package:flutter/material.dart';
import 'package:speakup/design/speak_up_design.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/auth_input.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/login_page.dart';

class RegisterPage extends StatefulWidget {
  const RegisterPage({
    required this.controller,
    required this.state,
    super.key,
  });

  final AuthController controller;
  final AuthSignedOut state;

  @override
  State<RegisterPage> createState() => _RegisterPageState();
}

class _RegisterPageState extends State<RegisterPage> {
  final _formKey = GlobalKey<FormState>();
  final _emailController = TextEditingController();
  final _displayNameController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _obscurePassword = true;

  @override
  void dispose() {
    _emailController.dispose();
    _displayNameController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AuthFormScaffold(
      title: '创建账号',
      subtitle: '从一次更自在的开口开始。',
      switchPrompt: '已经有账号？',
      switchActionLabel: '返回登录',
      onSwitch: widget.state.isSubmitting ? null : widget.controller.showLogin,
      errorMessage: widget.state.errorMessage,
      child: AutofillGroup(
        child: Form(
          key: _formKey,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              TextFormField(
                key: const Key('register-display-name'),
                controller: _displayNameController,
                textInputAction: TextInputAction.next,
                autofillHints: const [AutofillHints.name],
                textCapitalization: TextCapitalization.words,
                onTapOutside: (_) =>
                    FocusManager.instance.primaryFocus?.unfocus(),
                decoration: authFieldDecoration(label: '昵称'),
                validator: validateDisplayNameInput,
              ),
              const SizedBox(height: SpeakUpDesign.space16),
              AuthEmailField(controller: _emailController),
              const SizedBox(height: SpeakUpDesign.space16),
              AuthPasswordField(
                controller: _passwordController,
                obscureText: _obscurePassword,
                minimumLength: 8,
                autofillHint: AutofillHints.newPassword,
                helperText: '至少 8 个字符',
                onToggleVisibility: () =>
                    setState(() => _obscurePassword = !_obscurePassword),
                onSubmitted: (_) => _submit(),
              ),
              const SizedBox(height: SpeakUpDesign.space24),
              AuthPrimaryButton(
                label: '创建账号',
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
    widget.controller.register(
      email: normalizeIdentityEmailInput(_emailController.text),
      password: _passwordController.text,
      displayName: _displayNameController.text.trim(),
    );
  }
}
