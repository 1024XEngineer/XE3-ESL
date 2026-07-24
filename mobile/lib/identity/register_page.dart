import 'package:flutter/material.dart';
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
      title: 'Create your account',
      subtitle: 'Use your email and a secure password to get started.',
      errorMessage: widget.state.errorMessage,
      child: Form(
        key: _formKey,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            AuthEmailField(controller: _emailController),
            const SizedBox(height: 16),
            AuthPasswordField(
              controller: _passwordController,
              obscureText: _obscurePassword,
              onToggleVisibility: () =>
                  setState(() => _obscurePassword = !_obscurePassword),
            ),
            const SizedBox(height: 24),
            FilledButton(
              onPressed: widget.state.isSubmitting ? null : _submit,
              child: widget.state.isSubmitting
                  ? const AuthButtonProgress()
                  : const Text('Create account'),
            ),
            const SizedBox(height: 12),
            TextButton(
              onPressed: widget.state.isSubmitting
                  ? null
                  : widget.controller.showLogin,
              child: const Text('Back to sign in'),
            ),
          ],
        ),
      ),
    );
  }

  void _submit() {
    if (!(_formKey.currentState?.validate() ?? false)) {
      return;
    }
    widget.controller.register(
      email: normalizeIdentityEmailInput(_emailController.text),
      password: _passwordController.text,
    );
  }
}
