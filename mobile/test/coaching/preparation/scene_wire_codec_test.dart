import 'package:flutter_test/flutter_test.dart';
import 'package:speakup/features/coaching/scene/scene.dart';
import 'package:speakup/features/coaching/scene/scene_wire_codec.dart';

import '../../support/preparation_contract_fixtures.dart';

void main() {
  final selection = SceneSelectionSnapshot(
    source: SceneSource.catalog(
      sceneId: contractScene.id,
      sceneVersion: contractScene.version,
    ),
    scene: contractScene,
    selectedRoleIds: const <String>['technical-interviewer'],
    practiceOptionId: 'full-simulation',
  );

  test('decodes the current executable Scene selection snapshot', () {
    final decoded = decodeSceneSelectionSnapshot(
      encodeSceneSelectionSnapshot(selection),
    );

    expect(decoded.source.type, SceneSourceType.catalog);
    expect(decoded.source.sceneId, contractScene.id);
    expect(decoded.source.sceneVersion, contractScene.version);
    expect(decoded.scene.id, contractScene.id);
  });

  test('decodes a legacy catalog Scene selection snapshot', () {
    final decoded = decodeSceneSelectionSnapshot(_legacySelection());

    expect(decoded.source.type, SceneSourceType.catalog);
    expect(decoded.source.sceneId, contractScene.id);
    expect(decoded.source.sceneVersion, contractScene.version);
    expect(decoded.selectedRoleIds, <String>['technical-interviewer']);
    expect(decoded.practiceOptionId, 'full-simulation');
  });

  test('encoder writes only the current Scene selection shape', () {
    final encoded = encodeSceneSelectionSnapshot(selection);
    final scene = encoded['scene']! as Map<String, Object?>;

    expect(encoded.keys.toSet(), <String>{
      'source',
      'scene',
      'selected_role_ids',
      'practice_option_id',
    });
    expect(scene, containsPair('scene_key', contractScene.id));
    expect(scene.containsKey('scene_id'), isFalse);
    expect(scene.containsKey('status'), isFalse);
  });

  test('rejects mixed, extra, and incomplete Scene selection shapes', () {
    final current = encodeSceneSelectionSnapshot(selection);
    final legacy = _legacySelection();
    final cases = <String, Map<String, Object?>>{
      'mixed legacy scene with current source': <String, Object?>{
        ...legacy,
        'source': current['source'],
      },
      'extra legacy field': <String, Object?>{...legacy, 'unexpected': true},
      'missing current field': Map<String, Object?>.of(current)
        ..remove('practice_option_id'),
      'missing legacy field': Map<String, Object?>.of(legacy)
        ..remove('selected_role_ids'),
    };

    for (final entry in cases.entries) {
      expect(
        () => decodeSceneSelectionSnapshot(entry.value),
        throwsA(isA<SceneWireFormatException>()),
        reason: entry.key,
      );
    }
  });
}

Map<String, Object?> _legacySelection() => <String, Object?>{
  'scene': encodeSceneDefinition(contractScene),
  'selected_role_ids': <String>['technical-interviewer'],
  'practice_option_id': 'full-simulation',
};
