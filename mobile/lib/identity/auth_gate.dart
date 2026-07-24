import 'package:flutter/material.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/login_page.dart';
import 'package:speakup/identity/model/identity_models.dart';
import 'package:speakup/identity/register_page.dart';

typedef AuthenticatedBuilder = Widget Function(BuildContext context, User user);

class AuthGate extends StatefulWidget {
  const AuthGate({
    required this.controller,
    required this.authenticatedBuilder,
    this.initialize = true,
    super.key,
  });

  final AuthController controller;
  final AuthenticatedBuilder authenticatedBuilder;
  final bool initialize;

  @override
  State<AuthGate> createState() => _AuthGateState();
}

class _AuthGateState extends State<AuthGate> {
  @override
  void initState() {
    super.initState();
    widget.controller.addListener(_rebuild);
    if (widget.initialize) {
      widget.controller.initialize();
    }
  }

  @override
  void didUpdateWidget(covariant AuthGate oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller == widget.controller) {
      return;
    }
    oldWidget.controller.removeListener(_rebuild);
    widget.controller.addListener(_rebuild);
    if (widget.initialize) {
      widget.controller.initialize();
    }
  }

  @override
  void dispose() {
    widget.controller.removeListener(_rebuild);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return switch (widget.controller.state) {
      AuthLoading() => const _LoadingPage(),
      AuthSignedOut(form: AuthForm.login) ||
      AuthSignedOut(
        form: AuthForm.register,
      ) => _buildSignedOut(widget.controller.state as AuthSignedOut),
      AuthRetryableError(:final message) => _RetryPage(
        message: message,
        onRetry: widget.controller.retry,
      ),
      AuthAuthenticated(:final user) => widget.authenticatedBuilder(
        context,
        user,
      ),
    };
  }

  Widget _buildSignedOut(AuthSignedOut state) {
    return switch (state.form) {
      AuthForm.login => LoginPage(controller: widget.controller, state: state),
      AuthForm.register => RegisterPage(
        controller: widget.controller,
        state: state,
      ),
    };
  }

  void _rebuild() {
    if (mounted) {
      setState(() {});
    }
  }
}

class _LoadingPage extends StatelessWidget {
  const _LoadingPage();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Center(
        child: Semantics(
          label: 'Restoring your session',
          child: const CircularProgressIndicator(),
        ),
      ),
    );
  }
}

class _RetryPage extends StatelessWidget {
  const _RetryPage({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: Padding(
            padding: const EdgeInsets.all(32),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Icon(Icons.cloud_off_outlined, size: 40),
                  const SizedBox(height: 20),
                  Text(
                    'Connection needed',
                    style: Theme.of(context).textTheme.headlineSmall,
                  ),
                  const SizedBox(height: 8),
                  Text(message, textAlign: TextAlign.center),
                  const SizedBox(height: 24),
                  FilledButton(
                    onPressed: onRetry,
                    child: const Text('Try again'),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
