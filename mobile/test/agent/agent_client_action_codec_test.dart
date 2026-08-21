import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/agent/client_action/agent_client_action_codec.dart';

void main() {
  test('decodes a generic versioned client action envelope', () {
    final actions = decodeAgentClientActions(<Object?>[
      <String, Object?>{
        'type': 'resource.open.v1',
        'payload': <String, Object?>{'resource_id': 'resource-1'},
      },
    ]);

    expect(actions, hasLength(1));
    expect(actions.single.type, 'resource.open.v1');
    expect(actions.single.payload, <String, Object?>{
      'resource_id': 'resource-1',
    });
  });

  test('rejects malformed, unbounded, or ambiguous envelopes', () {
    final oversized = 'x' * (16 * 1024);
    for (final payload in <Object?>[
      <Object?>[
        <String, Object?>{'type': 'bad type', 'payload': <String, Object?>{}},
      ],
      <Object?>[
        <String, Object?>{'type': 'valid.v1', 'payload': <Object?>[]},
      ],
      <Object?>[
        <String, Object?>{
          'type': 'valid.v1',
          'payload': <String, Object?>{'value': oversized},
        },
      ],
      List<Object?>.generate(
        5,
        (_) => <String, Object?>{
          'type': 'valid.v1',
          'payload': <String, Object?>{},
        },
      ),
    ]) {
      expect(
        () => decodeAgentClientActions(payload),
        throwsA(isA<FormatException>()),
      );
    }
  });
}
