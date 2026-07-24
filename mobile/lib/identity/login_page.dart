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
      title: 'Welcome back',
      subtitle: 'Sign in to continue with SpeakUp.',
      message: widget.state.noticeMessage,
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
                  : const Text('Sign in'),
            ),
            const SizedBox(height: 12),
            TextButton(
              onPressed: widget.state.isSubmitting
                  ? null
                  : widget.controller.showRegister,
              child: const Text('Create an account'),
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
    required this.child,
    this.message,
    this.errorMessage,
    super.key,
  });

  final String title;
  final String subtitle;
  final String? message;
  final String? errorMessage;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Scaffold(
      backgroundColor: colors.surface,
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 32),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 440),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Icon(
                    Icons.record_voice_over_outlined,
                    size: 40,
                    color: colors.primary,
                  ),
                  const SizedBox(height: 20),
                  Text(
                    title,
                    textAlign: TextAlign.center,
                    style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    subtitle,
                    textAlign: TextAlign.center,
                    style: Theme.of(context).textTheme.bodyLarge?.copyWith(
                      color: colors.onSurfaceVariant,
                    ),
                  ),
                  if (message != null) ...[
                    const SizedBox(height: 20),
                    AuthMessage(message: message!),
                  ],
                  if (errorMessage != null) ...[
                    const SizedBox(height: 20),
                    AuthMessage(message: errorMessage!, isError: true),
                  ],
                  const SizedBox(height: 28),
                  child,
                ],
              ),
            ),
          ),
        ),
      ),
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
      decoration: const InputDecoration(
        labelText: 'Email',
        border: OutlineInputBorder(),
      ),
      validator: (value) {
        if (!isValidIdentityEmailInput(value ?? '')) {
          return 'Enter a valid email address.';
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
    required this.onToggleVisibility,
    super.key,
  });

  final TextEditingController controller;
  final bool obscureText;
  final VoidCallback onToggleVisibility;

  @override
  Widget build(BuildContext context) {
    return TextFormField(
      controller: controller,
      obscureText: obscureText,
      autofillHints: const [AutofillHints.password],
      enableSuggestions: false,
      autocorrect: false,
      decoration: InputDecoration(
        labelText: 'Password',
        helperText: '15–128 characters',
        border: const OutlineInputBorder(),
        suffixIcon: IconButton(
          onPressed: onToggleVisibility,
          tooltip: obscureText ? 'Show password' : 'Hide password',
          icon: Icon(
            obscureText
                ? Icons.visibility_outlined
                : Icons.visibility_off_outlined,
          ),
        ),
      ),
      validator: (value) {
        final length = value?.length ?? 0;
        if (length < 15 || length > 128) {
          return 'Password must be between 15 and 128 characters.';
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
      child: CircularProgressIndicator(strokeWidth: 2),
    );
  }
}

class AuthMessage extends StatelessWidget {
  const AuthMessage({required this.message, this.isError = false, super.key});

  final String message;
  final bool isError;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Semantics(
      liveRegion: true,
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: isError ? colors.errorContainer : colors.secondaryContainer,
          borderRadius: BorderRadius.circular(12),
        ),
        child: Text(
          message,
          style: TextStyle(
            color: isError
                ? colors.onErrorContainer
                : colors.onSecondaryContainer,
          ),
        ),
      ),
    );
  }
}
