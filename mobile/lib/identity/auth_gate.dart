import 'package:flutter/material.dart';
import 'package:speakup/identity/auth_controller.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/auth_input.dart';
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
      AuthRetryableError(:final message, :final action) => _RetryPage(
        message: message,
        onRetry: widget.controller.retry,
        onSwitchAccount: action == AuthRetryAction.restoreSession
            ? widget.controller.switchAccount
            : null,
      ),
      AuthAuthenticated(:final user) =>
        widget.controller.shouldPromptForProfile
            ? _ProfileCompletionPage(controller: widget.controller)
            : widget.authenticatedBuilder(context, user),
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

class _ProfileCompletionPage extends StatefulWidget {
  const _ProfileCompletionPage({required this.controller});

  final AuthController controller;

  @override
  State<_ProfileCompletionPage> createState() => _ProfileCompletionPageState();
}

class _ProfileCompletionPageState extends State<_ProfileCompletionPage> {
  final _formKey = GlobalKey<FormState>();
  final _displayNameController = TextEditingController();
  String? _errorMessage;

  @override
  void dispose() {
    _displayNameController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(28),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: Form(
                key: _formKey,
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text(
                      '怎么称呼你？',
                      style: Theme.of(context).textTheme.headlineMedium,
                    ),
                    const SizedBox(height: 8),
                    const Text('设置昵称后，SpeakUp 会在合适的时候用它称呼你。'),
                    const SizedBox(height: 24),
                    TextFormField(
                      key: const Key('complete-profile-display-name'),
                      controller: _displayNameController,
                      autofocus: true,
                      textInputAction: TextInputAction.done,
                      decoration: const InputDecoration(
                        labelText: '昵称',
                        border: OutlineInputBorder(),
                      ),
                      validator: validateDisplayNameInput,
                      onFieldSubmitted: (_) => _save(),
                    ),
                    if (_errorMessage != null) ...[
                      const SizedBox(height: 12),
                      Text(
                        _errorMessage!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                    ],
                    const SizedBox(height: 20),
                    FilledButton(
                      key: const Key('complete-profile-save'),
                      onPressed: widget.controller.profileSaving ? null : _save,
                      child: Text(
                        widget.controller.profileSaving ? '正在保存…' : '保存昵称',
                      ),
                    ),
                    TextButton(
                      key: const Key('complete-profile-skip'),
                      onPressed: widget.controller.profileSaving
                          ? null
                          : widget.controller.dismissProfilePrompt,
                      child: const Text('稍后再说'),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _save() async {
    if (!(_formKey.currentState?.validate() ?? false)) {
      return;
    }
    final error = await widget.controller.updateDisplayName(
      _displayNameController.text.trim(),
    );
    if (mounted) {
      setState(() => _errorMessage = error);
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
          label: '正在恢复登录状态',
          child: const CircularProgressIndicator(),
        ),
      ),
    );
  }
}

class _RetryPage extends StatelessWidget {
  const _RetryPage({
    required this.message,
    required this.onRetry,
    required this.onSwitchAccount,
  });

  final String message;
  final VoidCallback onRetry;
  final VoidCallback? onSwitchAccount;

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
                    '需要网络连接',
                    style: Theme.of(context).textTheme.headlineSmall,
                  ),
                  const SizedBox(height: 8),
                  Text(message, textAlign: TextAlign.center),
                  const SizedBox(height: 24),
                  FilledButton(onPressed: onRetry, child: const Text('重试')),
                  if (onSwitchAccount != null) ...[
                    const SizedBox(height: 8),
                    TextButton(
                      key: const Key('auth-switch-account'),
                      onPressed: onSwitchAccount,
                      child: const Text('使用其他账号'),
                    ),
                  ],
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
