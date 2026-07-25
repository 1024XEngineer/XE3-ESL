import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/agent/agent_client.dart';
import 'package:speakup/agent/agent_models.dart';
import 'package:speakup/agent/wire_agent_client.dart';
import 'package:speakup/identity/auth_state.dart';
import 'package:speakup/identity/network/identity_http_transport.dart';

void main() {
  group('WireAgentClient Thread restore', () {
    test('restores the latest durable Thread and ordered Messages', () async {
      final transport = _ScriptedTransport([
        _Step(
          method: 'GET',
          path: '/v1/agent-threads',
          response: _jsonResponse(HttpStatus.ok, {
            'threads': [_threadJson()],
          }),
        ),
        _Step(
          method: 'GET',
          path: '/v1/agent-threads/$_threadId/messages',
          response: _jsonResponse(HttpStatus.ok, {
            'messages': [
              _messageJson(
                id: _userMessageId,
                sequence: 1,
                role: 'user',
                content: 'Help me explain this.',
                clientMessageId: 'message_restore',
              ),
              _messageJson(
                id: _assistantMessageId,
                sequence: 2,
                role: 'assistant',
                content: 'Start with the result.',
                producedByRunId: _runId,
              ),
            ],
          }),
        ),
      ]);
      final harness = _Harness(transport);

      final snapshot = await harness.client.restoreThread();

      expect(snapshot.threadId, _threadId);
      expect(snapshot.messages, hasLength(2));
      expect(snapshot.messages.first.text, 'Help me explain this.');
      expect(snapshot.messages.last.role, AgentMessageRole.assistant);
      expect(
        transport.calls.every(
          (call) =>
              call.headers[HttpHeaders.authorizationHeader] ==
              'Bearer sess_account-a',
        ),
        isTrue,
      );
      transport.expectDone();
    });

    test('creates one Thread when the account has no history', () async {
      final transport = _ScriptedTransport([
        _Step(
          method: 'GET',
          path: '/v1/agent-threads',
          response: _jsonResponse(HttpStatus.ok, {'threads': <Object?>[]}),
        ),
        _Step(
          method: 'POST',
          path: '/v1/agent-threads',
          verify: (call) {
            expect(jsonDecode(call.body!), <String, Object?>{});
          },
          response: _jsonResponse(HttpStatus.created, _threadJson()),
        ),
        _Step(
          method: 'GET',
          path: '/v1/agent-threads/$_threadId/messages',
          response: _jsonResponse(HttpStatus.ok, {'messages': <Object?>[]}),
        ),
      ]);
      final harness = _Harness(transport);

      final snapshot = await harness.client.restoreThread();

      expect(snapshot.threadId, _threadId);
      expect(snapshot.messages, isEmpty);
      transport.expectDone();
    });

    test('restores a failed Run and its stable retry identity', () async {
      const text = 'Restore this failed request.';
      const clientMessageId = 'message_cold_restore';
      final transport = _ScriptedTransport([
        _Step(
          method: 'GET',
          path: '/v1/agent-threads',
          response: _jsonResponse(HttpStatus.ok, {
            'threads': [_threadJson()],
          }),
        ),
        _Step(
          method: 'GET',
          path: '/v1/agent-threads/$_threadId/messages',
          response: _jsonResponse(HttpStatus.ok, {
            'messages': [
              _messageJson(
                id: _userMessageId,
                sequence: 1,
                role: 'user',
                content: text,
                clientMessageId: clientMessageId,
              ),
            ],
          }),
        ),
        _Step(
          method: 'POST',
          path: '/v1/agent-threads/$_threadId/runs',
          verify: (call) {
            expect(jsonDecode(call.body!), {
              'client_message_id': clientMessageId,
              'content': text,
            });
          },
          response: _jsonResponse(
            HttpStatus.created,
            _runJson(
              status: 'failed',
              failureKind: 'timeout',
              failureRetryable: true,
            ),
          ),
        ),
        _Step(
          method: 'POST',
          path: '/v1/agent-runs/$_runId/retries',
          response: _jsonResponse(
            HttpStatus.created,
            _runJson(
              id: _retryRunId,
              status: 'completed',
              retryOfRunId: _runId,
              clientRetryId: 'retry:$_runId',
            ),
          ),
        ),
        _messagesStep(
          userContent: text,
          clientMessageId: clientMessageId,
          assistantRunId: _retryRunId,
        ),
      ]);
      final harness = _Harness(transport);

      final snapshot = await harness.client.restoreThread();

      expect(snapshot.messages, hasLength(1));
      expect(snapshot.textRecovery?.clientMessageId, clientMessageId);
      expect(snapshot.textRecovery?.retryable, isTrue);

      final exchange = await harness.client.sendText(
        threadId: _threadId,
        text: text,
        clientMessageId: clientMessageId,
      );

      expect(exchange.assistantMessage, isNotNull);
      expect(
        transport.calls
            .where((call) => call.path == '/v1/agent-threads/$_threadId/runs')
            .length,
        1,
      );
      transport.expectDone();
    });

    test(
      'finishes a restored pending Message without duplicating it',
      () async {
        const text = 'Finish this restored request.';
        final transport = _ScriptedTransport([
          _Step(
            method: 'GET',
            path: '/v1/agent-threads',
            response: _jsonResponse(HttpStatus.ok, {
              'threads': [_threadJson()],
            }),
          ),
          _Step(
            method: 'GET',
            path: '/v1/agent-threads/$_threadId/messages',
            response: _jsonResponse(HttpStatus.ok, {
              'messages': [
                _messageJson(
                  id: _userMessageId,
                  sequence: 1,
                  role: 'user',
                  content: text,
                  clientMessageId: 'message_pending_restore',
                ),
              ],
            }),
          ),
          _Step(
            method: 'POST',
            path: '/v1/agent-threads/$_threadId/runs',
            response: _jsonResponse(
              HttpStatus.accepted,
              _runJson(status: 'pending'),
            ),
          ),
          _Step(
            method: 'GET',
            path: '/v1/agent-runs/$_runId',
            response: _jsonResponse(
              HttpStatus.ok,
              _runJson(status: 'completed'),
            ),
          ),
          _messagesStep(
            userContent: text,
            clientMessageId: 'message_pending_restore',
          ),
        ]);
        final harness = _Harness(transport, maxRunPollAttempts: 2);

        final snapshot = await harness.client.restoreThread();

        expect(snapshot.textRecovery, isNull);
        expect(snapshot.messages, hasLength(2));
        expect(snapshot.messages.first.id, _userMessageId);
        expect(snapshot.messages.last.id, _assistantMessageId);
        transport.expectDone();
      },
    );

    test('rejects unknown response fields and unsafe ordering', () async {
      final malformedThread = _threadJson()..['unexpected'] = true;
      final transport = _ScriptedTransport([
        _Step(
          method: 'GET',
          path: '/v1/agent-threads',
          response: _jsonResponse(HttpStatus.ok, {
            'threads': [malformedThread],
          }),
        ),
      ]);
      final harness = _Harness(transport);

      await expectLater(
        harness.client.restoreThread(),
        throwsA(
          isA<AgentClientException>().having(
            (error) => error.kind,
            'kind',
            AgentClientFailureKind.invalidResponse,
          ),
        ),
      );
      transport.expectDone();
    });
  });

  group('WireAgentClient text Run', () {
    test(
      'accepts 3000 astral Unicode scalars within the API byte cap',
      () async {
        final text = List<String>.filled(3000, '😀').join();
        final transport = _ScriptedTransport([
          _Step(
            method: 'POST',
            path: '/v1/agent-threads/$_threadId/runs',
            response: _jsonResponse(
              HttpStatus.created,
              _runJson(status: 'completed'),
            ),
          ),
          _messagesStep(userContent: text, clientMessageId: 'message_unicode'),
        ]);
        final harness = _Harness(transport);

        final exchange = await harness.client.sendText(
          threadId: _threadId,
          text: text,
          clientMessageId: 'message_unicode',
        );

        expect(exchange.userMessage.text.runes, hasLength(3000));
        transport.expectDone();
      },
    );

    test(
      'submits a 201 Run and returns committed PostgreSQL Messages',
      () async {
        const text = 'How can I make this answer specific?';
        final transport = _ScriptedTransport([
          _Step(
            method: 'POST',
            path: '/v1/agent-threads/$_threadId/runs',
            verify: (call) {
              expect(jsonDecode(call.body!), {
                'client_message_id': 'message_201',
                'content': text,
              });
            },
            response: _jsonResponse(
              HttpStatus.created,
              _runJson(status: 'completed'),
            ),
          ),
          _messagesStep(userContent: text, clientMessageId: 'message_201'),
        ]);
        final harness = _Harness(transport);

        final exchange = await harness.client.sendText(
          threadId: _threadId,
          text: text,
          clientMessageId: 'message_201',
        );

        expect(exchange.userMessage.id, _userMessageId);
        expect(exchange.userMessage.text, text);
        expect(exchange.assistantMessage?.id, _assistantMessageId);
        expect(exchange.assistantMessage?.text, 'Use one measurable result.');
        transport.expectDone();
      },
    );

    test(
      'rejects completed Messages that do not match the submitted request',
      () async {
        const text = 'Keep this request paired.';
        const clientMessageId = 'message_pairing';
        final cases =
            <
              ({
                String name,
                String userContent,
                String userClientMessageId,
                int userSequence,
                int assistantSequence,
                String assistantRunId,
                bool assistantFirst,
              })
            >[
              (
                name: 'user content differs',
                userContent: 'Tampered request.',
                userClientMessageId: clientMessageId,
                userSequence: 1,
                assistantSequence: 2,
                assistantRunId: _runId,
                assistantFirst: false,
              ),
              (
                name: 'client Message identity differs',
                userContent: text,
                userClientMessageId: 'message_other',
                userSequence: 1,
                assistantSequence: 2,
                assistantRunId: _runId,
                assistantFirst: false,
              ),
              (
                name: 'assistant precedes the user Message',
                userContent: text,
                userClientMessageId: clientMessageId,
                userSequence: 2,
                assistantSequence: 1,
                assistantRunId: _runId,
                assistantFirst: true,
              ),
              (
                name: 'assistant belongs to another Run',
                userContent: text,
                userClientMessageId: clientMessageId,
                userSequence: 1,
                assistantSequence: 2,
                assistantRunId: _thirdRunId,
                assistantFirst: false,
              ),
            ];

        for (final pairingCase in cases) {
          final userMessage = _messageJson(
            id: _userMessageId,
            sequence: pairingCase.userSequence,
            role: 'user',
            content: pairingCase.userContent,
            clientMessageId: pairingCase.userClientMessageId,
          );
          final assistantMessage = _messageJson(
            id: _assistantMessageId,
            sequence: pairingCase.assistantSequence,
            role: 'assistant',
            content: 'This response must remain paired.',
            producedByRunId: pairingCase.assistantRunId,
          );
          final transport = _ScriptedTransport([
            _Step(
              method: 'POST',
              path: '/v1/agent-threads/$_threadId/runs',
              response: _jsonResponse(
                HttpStatus.created,
                _runJson(status: 'completed'),
              ),
            ),
            _Step(
              method: 'GET',
              path: '/v1/agent-threads/$_threadId/messages',
              response: _jsonResponse(HttpStatus.ok, {
                'messages': pairingCase.assistantFirst
                    ? [assistantMessage, userMessage]
                    : [userMessage, assistantMessage],
              }),
            ),
          ]);
          final harness = _Harness(transport, maxMessagePollAttempts: 1);

          await expectLater(
            harness.client.sendText(
              threadId: _threadId,
              text: text,
              clientMessageId: clientMessageId,
            ),
            throwsA(
              isA<AgentClientException>().having(
                (error) => error.kind,
                'kind',
                AgentClientFailureKind.invalidResponse,
              ),
            ),
            reason: pairingCase.name,
          );
          transport.expectDone();
        }
      },
    );

    test(
      'polls a 202 Run within a fixed bound before loading Messages',
      () async {
        const text = 'Please continue.';
        final transport = _ScriptedTransport([
          _Step(
            method: 'POST',
            path: '/v1/agent-threads/$_threadId/runs',
            response: _jsonResponse(
              HttpStatus.accepted,
              _runJson(status: 'pending'),
            ),
          ),
          _Step(
            method: 'GET',
            path: '/v1/agent-runs/$_runId',
            response: _jsonResponse(HttpStatus.ok, _runJson(status: 'running')),
          ),
          _Step(
            method: 'GET',
            path: '/v1/agent-runs/$_runId',
            response: _jsonResponse(
              HttpStatus.ok,
              _runJson(status: 'completed'),
            ),
          ),
          _messagesStep(userContent: text, clientMessageId: 'message_202'),
        ]);
        final harness = _Harness(transport, maxRunPollAttempts: 3);

        final exchange = await harness.client.sendText(
          threadId: _threadId,
          text: text,
          clientMessageId: 'message_202',
        );

        expect(exchange.assistantMessage, isNotNull);
        transport.expectDone();
      },
    );

    test(
      'uses the explicit retry endpoint after a retryable failed Run',
      () async {
        const text = 'Retry this exact message.';
        final transport = _ScriptedTransport([
          _Step(
            method: 'POST',
            path: '/v1/agent-threads/$_threadId/runs',
            response: _jsonResponse(
              HttpStatus.created,
              _runJson(
                status: 'failed',
                failureKind: 'internal_error',
                failureRetryable: true,
              ),
            ),
          ),
          _Step(
            method: 'POST',
            path: '/v1/agent-runs/$_runId/retries',
            verify: (call) {
              expect(jsonDecode(call.body!), {
                'client_retry_id': 'retry:$_runId',
              });
            },
            response: _jsonResponse(
              HttpStatus.created,
              _runJson(
                id: _retryRunId,
                status: 'completed',
                retryOfRunId: _runId,
                clientRetryId: 'retry:$_runId',
              ),
            ),
          ),
          _messagesStep(
            userContent: text,
            clientMessageId: 'message_retry',
            assistantRunId: _retryRunId,
          ),
        ]);
        final harness = _Harness(transport);

        await expectLater(
          harness.client.sendText(
            threadId: _threadId,
            text: text,
            clientMessageId: 'message_retry',
          ),
          throwsA(
            isA<AgentClientException>()
                .having(
                  (error) => error.kind,
                  'kind',
                  AgentClientFailureKind.runFailed,
                )
                .having((error) => error.retryable, 'retryable', isTrue),
          ),
        );

        final exchange = await harness.client.sendText(
          threadId: _threadId,
          text: text,
          clientMessageId: 'message_retry',
        );

        expect(exchange.assistantMessage?.text, 'Use one measurable result.');
        expect(
          transport.calls
              .where((call) => call.path == '/v1/agent-threads/$_threadId/runs')
              .length,
          1,
        );
        transport.expectDone();
      },
    );

    test(
      'one retry recovers an ambiguous submit and creates a new attempt',
      () async {
        const text = 'Recover this interrupted request.';
        final transport = _ScriptedTransport([
          const _Step(
            method: 'POST',
            path: '/v1/agent-threads/$_threadId/runs',
            error: SocketException('connection interrupted'),
          ),
          _Step(
            method: 'POST',
            path: '/v1/agent-threads/$_threadId/runs',
            response: _jsonResponse(
              HttpStatus.created,
              _runJson(
                status: 'failed',
                failureKind: 'interrupted',
                failureRetryable: true,
              ),
            ),
          ),
          _Step(
            method: 'POST',
            path: '/v1/agent-runs/$_runId/retries',
            response: _jsonResponse(
              HttpStatus.created,
              _runJson(
                id: _retryRunId,
                status: 'completed',
                retryOfRunId: _runId,
                clientRetryId: 'retry:$_runId',
              ),
            ),
          ),
          _messagesStep(
            userContent: text,
            clientMessageId: 'message_ambiguous',
            assistantRunId: _retryRunId,
          ),
        ]);
        final harness = _Harness(transport);

        await expectLater(
          harness.client.sendText(
            threadId: _threadId,
            text: text,
            clientMessageId: 'message_ambiguous',
          ),
          throwsA(
            isA<AgentClientException>().having(
              (error) => error.kind,
              'kind',
              AgentClientFailureKind.network,
            ),
          ),
        );

        final exchange = await harness.client.sendText(
          threadId: _threadId,
          text: text,
          clientMessageId: 'message_ambiguous',
        );

        expect(exchange.assistantMessage, isNotNull);
        expect(transport.calls.map((call) => call.path), [
          '/v1/agent-threads/$_threadId/runs',
          '/v1/agent-threads/$_threadId/runs',
          '/v1/agent-runs/$_runId/retries',
          '/v1/agent-threads/$_threadId/messages',
        ]);
        transport.expectDone();
      },
    );

    test(
      'one retry recovers an ambiguous pending run that later fails',
      () async {
        const text = 'Recover this pending interrupted request.';
        final transport = _ScriptedTransport([
          const _Step(
            method: 'POST',
            path: '/v1/agent-threads/$_threadId/runs',
            error: SocketException('connection interrupted'),
          ),
          _Step(
            method: 'POST',
            path: '/v1/agent-threads/$_threadId/runs',
            response: _jsonResponse(
              HttpStatus.accepted,
              _runJson(status: 'pending'),
            ),
          ),
          _Step(
            method: 'GET',
            path: '/v1/agent-runs/$_runId',
            response: _jsonResponse(
              HttpStatus.ok,
              _runJson(
                status: 'failed',
                failureKind: 'interrupted',
                failureRetryable: true,
              ),
            ),
          ),
          _Step(
            method: 'POST',
            path: '/v1/agent-runs/$_runId/retries',
            response: _jsonResponse(
              HttpStatus.created,
              _runJson(
                id: _retryRunId,
                status: 'completed',
                retryOfRunId: _runId,
                clientRetryId: 'retry:$_runId',
              ),
            ),
          ),
          _messagesStep(
            userContent: text,
            clientMessageId: 'message_ambiguous_pending',
            assistantRunId: _retryRunId,
          ),
        ]);
        final harness = _Harness(transport);

        await expectLater(
          harness.client.sendText(
            threadId: _threadId,
            text: text,
            clientMessageId: 'message_ambiguous_pending',
          ),
          throwsA(
            isA<AgentClientException>().having(
              (error) => error.kind,
              'kind',
              AgentClientFailureKind.network,
            ),
          ),
        );

        final exchange = await harness.client.sendText(
          threadId: _threadId,
          text: text,
          clientMessageId: 'message_ambiguous_pending',
        );

        expect(exchange.assistantMessage, isNotNull);
        expect(transport.calls.map((call) => call.path), [
          '/v1/agent-threads/$_threadId/runs',
          '/v1/agent-threads/$_threadId/runs',
          '/v1/agent-runs/$_runId',
          '/v1/agent-runs/$_runId/retries',
          '/v1/agent-threads/$_threadId/messages',
        ]);
        transport.expectDone();
      },
    );

    test('advances retry identity after each failed attempt', () async {
      const text = 'This may need more than one attempt.';
      final transport = _ScriptedTransport([
        _Step(
          method: 'POST',
          path: '/v1/agent-threads/$_threadId/runs',
          response: _jsonResponse(
            HttpStatus.created,
            _runJson(
              status: 'failed',
              failureKind: 'internal_error',
              failureRetryable: true,
            ),
          ),
        ),
        _Step(
          method: 'POST',
          path: '/v1/agent-runs/$_runId/retries',
          response: _jsonResponse(
            HttpStatus.created,
            _runJson(
              id: _retryRunId,
              status: 'failed',
              failureKind: 'internal_error',
              failureRetryable: true,
              retryOfRunId: _runId,
              clientRetryId: 'retry:$_runId',
            ),
          ),
        ),
        _Step(
          method: 'POST',
          path: '/v1/agent-runs/$_retryRunId/retries',
          verify: (call) {
            expect(jsonDecode(call.body!), {
              'client_retry_id': 'retry:$_retryRunId',
            });
          },
          response: _jsonResponse(
            HttpStatus.created,
            _runJson(
              id: _thirdRunId,
              status: 'completed',
              retryOfRunId: _retryRunId,
              clientRetryId: 'retry:$_retryRunId',
            ),
          ),
        ),
        _messagesStep(
          userContent: text,
          clientMessageId: 'message_retry_chain',
          assistantRunId: _thirdRunId,
        ),
      ]);
      final harness = _Harness(transport);

      for (var attempt = 0; attempt < 2; attempt++) {
        await expectLater(
          harness.client.sendText(
            threadId: _threadId,
            text: text,
            clientMessageId: 'message_retry_chain',
          ),
          throwsA(
            isA<AgentClientException>().having(
              (error) => error.kind,
              'kind',
              AgentClientFailureKind.runFailed,
            ),
          ),
        );
      }
      final exchange = await harness.client.sendText(
        threadId: _threadId,
        text: text,
        clientMessageId: 'message_retry_chain',
      );

      expect(exchange.assistantMessage, isNotNull);
      transport.expectDone();
    });

    test('stops polling and exposes a retryable timeout', () async {
      final transport = _ScriptedTransport([
        _Step(
          method: 'POST',
          path: '/v1/agent-threads/$_threadId/runs',
          response: _jsonResponse(
            HttpStatus.accepted,
            _runJson(status: 'pending'),
          ),
        ),
        for (var index = 0; index < 2; index++)
          _Step(
            method: 'GET',
            path: '/v1/agent-runs/$_runId',
            response: _jsonResponse(HttpStatus.ok, _runJson(status: 'pending')),
          ),
      ]);
      final harness = _Harness(transport, maxRunPollAttempts: 3);

      await expectLater(
        harness.client.sendText(
          threadId: _threadId,
          text: 'Keep this bounded.',
          clientMessageId: 'message_timeout',
        ),
        throwsA(
          isA<AgentClientException>()
              .having(
                (error) => error.kind,
                'kind',
                AgentClientFailureKind.pollingTimedOut,
              )
              .having((error) => error.retryable, 'retryable', isTrue),
        ),
      );
      transport.expectDone();
    });
  });

  group('WireAgentClient account isolation', () {
    test('a current 401 triggers generation-aware invalidation', () async {
      final transport = _ScriptedTransport([
        _Step(
          method: 'GET',
          path: '/v1/agent-threads',
          response: _jsonResponse(HttpStatus.unauthorized, _errorJson()),
        ),
      ]);
      final harness = _Harness(transport);

      await expectLater(
        harness.client.restoreThread(),
        throwsA(
          isA<AgentClientException>().having(
            (error) => error.kind,
            'kind',
            AgentClientFailureKind.authenticationRequired,
          ),
        ),
      );
      await Future<void>.delayed(Duration.zero);

      expect(harness.invalidations, hasLength(1));
      expect(harness.invalidations.single.sessionToken, 'sess_account-a');
      expect(harness.invalidations.single.generation, 1);
      transport.expectDone();
    });

    test('a late account A 401 cannot invalidate account B', () async {
      final transport = _ControlledTransport();
      final pending = transport.enqueue();
      final harness = _Harness(transport);

      final restore = harness.client.restoreThread();
      await transport.waitForCalls(1);
      harness.credential = const AuthSessionCredential(
        sessionToken: 'sess_account-b',
        generation: 2,
      );
      pending.complete(_jsonResponse(HttpStatus.unauthorized, _errorJson()));

      await expectLater(restore, throwsA(isA<AgentClientOperationCancelled>()));
      await Future<void>.delayed(Duration.zero);
      expect(harness.invalidations, isEmpty);
    });

    test(
      'cleanup waits for old requests and B never receives A data',
      () async {
        final transport = _ControlledTransport();
        final accountAResponse = transport.enqueue();
        final harness = _Harness(transport);
        final staleRestore = harness.client.restoreThread();
        await transport.waitForCalls(1);

        var cleanupFinished = false;
        final cleanup = harness.client.clearAccountState();
        cleanup.whenComplete(() => cleanupFinished = true);
        await Future<void>.delayed(Duration.zero);
        expect(cleanupFinished, isFalse);

        accountAResponse.complete(
          _jsonResponse(HttpStatus.ok, {
            'threads': [_threadJson()],
          }),
        );
        await expectLater(
          staleRestore,
          throwsA(isA<AgentClientOperationCancelled>()),
        );
        await cleanup;

        harness.credential = const AuthSessionCredential(
          sessionToken: 'sess_account-b',
          generation: 2,
        );
        final accountBThreads = transport.enqueue()
          ..complete(
            _jsonResponse(HttpStatus.ok, {
              'threads': [_threadJson(id: _threadBId)],
            }),
          );
        final accountBMessages = transport.enqueue()
          ..complete(_jsonResponse(HttpStatus.ok, {'messages': <Object?>[]}));
        expect(accountBThreads.isCompleted, isTrue);
        expect(accountBMessages.isCompleted, isTrue);

        final accountB = await harness.client.restoreThread();

        expect(accountB.threadId, _threadBId);
        expect(
          transport.calls.last.headers[HttpHeaders.authorizationHeader],
          'Bearer sess_account-b',
        );
      },
    );

    test(
      'diagnostics never retain credentials or response body text',
      () async {
        const sensitiveBody =
            '{"error":{"code":"sess_account-a","message":"private prompt '
            'private prompt","retryable":true,"correlation_id":"corr_safe"}}';
        final transport = _ScriptedTransport([
          const _Step(
            method: 'GET',
            path: '/v1/agent-threads',
            response: IdentityHttpResponse(
              statusCode: HttpStatus.internalServerError,
              body: sensitiveBody,
            ),
          ),
        ]);
        final harness = _Harness(transport);

        late AgentClientException captured;
        try {
          await harness.client.restoreThread();
          fail('Expected a server failure.');
        } on AgentClientException catch (error) {
          captured = error;
        }

        expect(captured.errorCode, 'internal_error');
        expect(captured.toString(), isNot(contains('sess_account-a')));
        expect(captured.toString(), isNot(contains('private prompt')));
        expect(captured.toString(), isNot(contains(sensitiveBody)));
        transport.expectDone();
      },
    );

    test('owned transport rejects a response body larger than 1 MiB', () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final subscription = server.listen((request) async {
        try {
          request.response.statusCode = HttpStatus.ok;
          request.response.contentLength = 1024 * 1024 + 1;
          request.response.add(List<int>.filled(1024 * 1024 + 1, 65));
          await request.response.close();
        } catch (_) {
          // The client intentionally aborts this oversized response.
        }
      });
      addTearDown(() async {
        await subscription.cancel();
        await server.close(force: true);
      });
      final client = WireAgentClient(
        baseUri: Uri.parse('http://127.0.0.1:${server.port}'),
        credentialProvider: () => const AuthSessionCredential(
          sessionToken: 'sess_account-a',
          generation: 1,
        ),
        invalidateSession:
            ({
              required expectedSessionToken,
              required expectedGeneration,
            }) async {},
      );
      addTearDown(client.clearAccountState);

      await expectLater(
        client.restoreThread(),
        throwsA(
          isA<AgentClientException>().having(
            (error) => error.kind,
            'kind',
            AgentClientFailureKind.invalidResponse,
          ),
        ),
      );
    });
  });

  test('scene selection uses Matter and SetActiveMatter HTTP APIs', () async {
    final transport = _ScriptedTransport([
      _Step(
        method: 'GET',
        path: '/v1/matters',
        response: _jsonResponse(HttpStatus.ok, {
          'matters': [
            {
              'matter_id': _matterId,
              'title': agentScenes.first.title,
              'status': 'active',
              'version': 1,
              'created_at': '2026-07-25T09:00:00Z',
              'updated_at': '2026-07-25T09:00:00Z',
            },
          ],
        }),
      ),
      _Step(
        method: 'PUT',
        path: '/v1/agent-threads/$_threadId/active-matter',
        response: _jsonResponse(HttpStatus.ok, {
          'thread_id': _threadId,
          'matter_id': _matterId,
          'active': true,
          'linked_at': '2026-07-25T09:00:00Z',
          'updated_at': '2026-07-25T09:00:00Z',
        }),
      ),
    ]);
    final harness = _Harness(transport);

    expect(harness.client.supportsPracticeFlow, isFalse);
    final result = await harness.client.startScene(
      threadId: _threadId,
      scene: agentScenes.first,
      clientOperationId: 'scene_select',
    );
    expect(result.activeMatter.id, _matterId);
    await expectLater(
      harness.client.transcribeTurn(
        threadId: _threadId,
        turnNumber: 1,
        clientTurnId: 'turn_unavailable',
      ),
      throwsA(isA<AgentClientException>()),
    );
    transport.expectDone();
  });
}

final class _Harness {
  _Harness(
    IdentityHttpTransport transport, {
    int maxRunPollAttempts = 20,
    int maxMessagePollAttempts = 4,
  }) {
    client = WireAgentClient(
      baseUri: Uri.parse('https://api.speak-up.test'),
      credentialProvider: () => credential,
      invalidateSession:
          ({required expectedSessionToken, required expectedGeneration}) async {
            invalidations.add(
              AuthSessionCredential(
                sessionToken: expectedSessionToken,
                generation: expectedGeneration,
              ),
            );
          },
      transport: transport,
      pollInterval: Duration.zero,
      maxRunPollAttempts: maxRunPollAttempts,
      maxMessagePollAttempts: maxMessagePollAttempts,
    );
  }

  AuthSessionCredential? credential = const AuthSessionCredential(
    sessionToken: 'sess_account-a',
    generation: 1,
  );
  final List<AuthSessionCredential> invalidations = <AuthSessionCredential>[];
  late final WireAgentClient client;
}

final class _Step {
  const _Step({
    required this.method,
    required this.path,
    this.response,
    this.error,
    this.verify,
  }) : assert((response == null) != (error == null));

  final String method;
  final String path;
  final IdentityHttpResponse? response;
  final Object? error;
  final void Function(_Call call)? verify;
}

final class _Call {
  const _Call({
    required this.method,
    required this.path,
    required this.headers,
    required this.body,
  });

  final String method;
  final String path;
  final Map<String, String> headers;
  final String? body;
}

final class _ScriptedTransport implements IdentityHttpTransport {
  _ScriptedTransport(Iterable<_Step> steps) : _steps = Queue<_Step>.of(steps);

  final Queue<_Step> _steps;
  final List<_Call> calls = <_Call>[];

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) async {
    if (_steps.isEmpty) {
      throw StateError('Unexpected Agent HTTP request.');
    }
    final step = _steps.removeFirst();
    final call = _Call(
      method: method,
      path: uri.path,
      headers: Map<String, String>.of(headers),
      body: body,
    );
    calls.add(call);
    expect(method, step.method);
    expect(uri.path, step.path);
    step.verify?.call(call);
    if (step.error case final error?) {
      throw error;
    }
    return step.response!;
  }

  void expectDone() {
    expect(_steps, isEmpty);
  }
}

final class _ControlledTransport implements IdentityHttpTransport {
  final Queue<Completer<IdentityHttpResponse>> _responses =
      Queue<Completer<IdentityHttpResponse>>();
  final List<_Call> calls = <_Call>[];

  Completer<IdentityHttpResponse> enqueue() {
    final completer = Completer<IdentityHttpResponse>();
    _responses.add(completer);
    return completer;
  }

  Future<void> waitForCalls(int count) async {
    while (calls.length < count) {
      await Future<void>.delayed(Duration.zero);
    }
  }

  @override
  Future<IdentityHttpResponse> send({
    required String method,
    required Uri uri,
    required Map<String, String> headers,
    String? body,
  }) {
    if (_responses.isEmpty) {
      throw StateError('No controlled Agent response was queued.');
    }
    calls.add(
      _Call(
        method: method,
        path: uri.path,
        headers: Map<String, String>.of(headers),
        body: body,
      ),
    );
    return _responses.removeFirst().future;
  }
}

_Step _messagesStep({
  required String userContent,
  required String clientMessageId,
  String assistantRunId = _runId,
}) {
  return _Step(
    method: 'GET',
    path: '/v1/agent-threads/$_threadId/messages',
    response: _jsonResponse(HttpStatus.ok, {
      'messages': [
        _messageJson(
          id: _userMessageId,
          sequence: 1,
          role: 'user',
          content: userContent,
          clientMessageId: clientMessageId,
        ),
        _messageJson(
          id: _assistantMessageId,
          sequence: 2,
          role: 'assistant',
          content: 'Use one measurable result.',
          producedByRunId: assistantRunId,
        ),
      ],
    }),
  );
}

Map<String, Object?> _threadJson({String id = _threadId}) {
  return {'thread_id': id, 'created_at': _createdAt, 'updated_at': _updatedAt};
}

Map<String, Object?> _messageJson({
  required String id,
  required int sequence,
  required String role,
  required String content,
  String? clientMessageId,
  String? producedByRunId,
}) {
  return {
    'message_id': id,
    'thread_id': _threadId,
    'sequence': sequence,
    'role': role,
    'client_message_id': ?clientMessageId,
    'produced_by_run_id': ?producedByRunId,
    'content': content,
    'created_at': _createdAt,
  };
}

Map<String, Object?> _runJson({
  String id = _runId,
  required String status,
  String? failureKind,
  bool failureRetryable = false,
  String? retryOfRunId,
  String? clientRetryId,
}) {
  return {
    'run_id': id,
    'thread_id': _threadId,
    'input_message_id': _userMessageId,
    'attempt': retryOfRunId == null ? 1 : 2,
    'retry_of_run_id': ?retryOfRunId,
    'client_retry_id': ?clientRetryId,
    'status': status,
    'requested_provider': 'qianwen',
    'requested_model': 'qwen-flash',
    'max_output_tokens': 512,
    if (status == 'running' || status == 'completed' || status == 'failed')
      'started_at': _startedAt,
    if (status == 'completed' || status == 'failed')
      'completed_at': _completedAt,
    if (status == 'completed') ...{
      'assistant_message_id': _assistantMessageId,
      'provider_completion_id': 'completion_1',
      'provider_model': 'qwen-flash',
      'finish_reason': 'stop',
      'usage': {'input_tokens': 8, 'output_tokens': 5, 'total_tokens': 13},
    },
    if (status == 'failed')
      'failure': {
        'kind': failureKind ?? 'internal_error',
        'retryable': failureRetryable,
      },
    'created_at': _createdAt,
    'updated_at': _updatedAt,
  };
}

Map<String, Object?> _errorJson() {
  return {
    'error': {
      'code': 'authentication_required',
      'message': 'Authentication is required.',
      'retryable': false,
      'correlation_id': 'corr_test',
    },
  };
}

IdentityHttpResponse _jsonResponse(int statusCode, Object? body) {
  return IdentityHttpResponse(statusCode: statusCode, body: jsonEncode(body));
}

const _threadId = '10000000-0000-4000-8000-000000000001';
const _matterId = '40000000-0000-4000-8000-000000000001';
const _threadBId = '10000000-0000-4000-8000-000000000002';
const _userMessageId = '20000000-0000-4000-8000-000000000001';
const _assistantMessageId = '20000000-0000-4000-8000-000000000002';
const _runId = '30000000-0000-4000-8000-000000000001';
const _retryRunId = '30000000-0000-4000-8000-000000000002';
const _thirdRunId = '30000000-0000-4000-8000-000000000003';
const _createdAt = '2026-07-25T09:00:00Z';
const _startedAt = '2026-07-25T09:00:01Z';
const _completedAt = '2026-07-25T09:00:02Z';
const _updatedAt = '2026-07-25T09:00:03Z';
