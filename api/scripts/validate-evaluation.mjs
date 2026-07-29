import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';

import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';

const execFileAsync = promisify(execFile);
const apiDirectory = fileURLToPath(new URL('..', import.meta.url));
const fixture = JSON.parse(
  await readFile(
    resolve(apiDirectory, 'examples/evaluation-contract.json'),
    'utf8',
  ),
);

const bundleOpenApi = async () => {
  const temporaryDirectory = await mkdtemp(
    resolve(tmpdir(), 'speakup-evaluation-bundle-'),
  );
  const bundlePath = resolve(temporaryDirectory, 'openapi.bundle.json');
  const redoclyCliPath = resolve(
    apiDirectory,
    'node_modules/@redocly/cli/bin/cli.js',
  );

  try {
    await execFileAsync(
      process.execPath,
      [
        redoclyCliPath,
        'bundle',
        resolve(apiDirectory, 'openapi.yaml'),
        '--config',
        resolve(apiDirectory, 'redocly.yaml'),
        '--output',
        bundlePath,
        '--ext',
        'json',
      ],
      {
        cwd: apiDirectory,
        maxBuffer: 10 * 1024 * 1024,
      },
    );
    return JSON.parse(await readFile(bundlePath, 'utf8'));
  } finally {
    await rm(temporaryDirectory, { recursive: true, force: true });
  }
};

const decodeJsonPointerToken = (token) =>
  token.replaceAll('~1', '/').replaceAll('~0', '~');
const encodeJsonPointerToken = (token) =>
  token.replaceAll('~', '~0').replaceAll('/', '~1');
const componentReferencePrefix = '#/components/schemas/';

const findComponentReferences = (value, references = new Set()) => {
  if (Array.isArray(value)) {
    for (const item of value) {
      findComponentReferences(item, references);
    }
    return references;
  }
  if (value === null || typeof value !== 'object') {
    return references;
  }
  if (
    typeof value.$ref === 'string' &&
    value.$ref.startsWith(componentReferencePrefix)
  ) {
    references.add(
      decodeJsonPointerToken(value.$ref.slice(componentReferencePrefix.length)),
    );
  }
  for (const item of Object.values(value)) {
    findComponentReferences(item, references);
  }
  return references;
};

const rewriteComponentReferences = (value) => {
  if (Array.isArray(value)) {
    return value.map(rewriteComponentReferences);
  }
  if (value === null || typeof value !== 'object') {
    return value;
  }
  return Object.fromEntries(
    Object.entries(value).map(([key, item]) => {
      if (
        key === '$ref' &&
        typeof item === 'string' &&
        item.startsWith(componentReferencePrefix)
      ) {
        const schemaName = decodeJsonPointerToken(
          item.slice(componentReferencePrefix.length),
        );
        return [key, `#/$defs/${encodeJsonPointerToken(schemaName)}`];
      }
      return [key, rewriteComponentReferences(item)];
    }),
  );
};

const openApiBundle = await bundleOpenApi();
const componentSchemas = openApiBundle.components?.schemas;
assert.ok(componentSchemas, 'Bundled OpenAPI must contain component schemas.');

const buildSchema = (rootSchemaName) => {
  const definitions = {};
  const collect = (schemaName) => {
    if (Object.hasOwn(definitions, schemaName)) {
      return;
    }
    const schema = componentSchemas[schemaName];
    assert.ok(schema, `Missing bundled ${schemaName} schema.`);
    definitions[schemaName] = rewriteComponentReferences(schema);
    for (const dependencyName of findComponentReferences(schema)) {
      collect(dependencyName);
    }
  };
  collect(rootSchemaName);
  return {
    $schema: 'https://json-schema.org/draft/2020-12/schema',
    $ref: `#/$defs/${encodeJsonPointerToken(rootSchemaName)}`,
    $defs: definitions,
  };
};

const ajv = new Ajv2020({
  allErrors: true,
  strict: true,
  strictRequired: false,
});
addFormats(ajv);

const validators = Object.fromEntries(
  [
    'CreateEvaluationRequest',
    'EvaluationAccepted',
    'Evaluation',
    'SceneEvaluationResult',
    'CoreAbilityObservation',
    'EvidenceRef',
  ].map((schemaName) => [schemaName, ajv.compile(buildSchema(schemaName))]),
);

const validationErrors = (validator) =>
  (validator.errors ?? [])
    .map(
      ({ instancePath, keyword, message }) =>
        `${instancePath || '/'} ${keyword}: ${message}`,
    )
    .join('\n');

const assertValid = (caseName, schemaName, value) => {
  const validator = validators[schemaName];
  assert.equal(
    validator(value),
    true,
    `${caseName} violates ${schemaName}:\n${validationErrors(validator)}`,
  );
};

const assertSchemaRejected = (caseName, schemaName, value) => {
  const validator = validators[schemaName];
  assert.equal(
    validator(value),
    false,
    `${caseName}: invalid ${schemaName} fixture was accepted`,
  );
};

assertValid(
  'dual-channel create',
  'CreateEvaluationRequest',
  fixture.create_dual_channel,
);
assertValid('queued create response', 'EvaluationAccepted', fixture.queued);
assert.match(
  fixture.queued.evaluation_id,
  /^[0-9]/,
  'queued fixture must cover a digit-leading Evaluation UUID',
);
assertValid('Core 4D ready', 'Evaluation', fixture.core_4d_ready);
assert.match(
  fixture.core_4d_ready.evaluation_id,
  /^[A-Fa-f]/,
  'ready fixture must cover a letter-leading Evaluation UUID',
);
assertValid(
  'short sample blocked',
  'Evaluation',
  fixture.short_sample_blocked,
);
assertValid(
  'low ASR feedback only',
  'Evaluation',
  fixture.low_asr_feedback_only,
);
assertValid('IELTS ready', 'Evaluation', fixture.ielts_ready);
assertValid(
  'Interview missing opportunity',
  'Evaluation',
  fixture.interview_missing_opportunity,
);
assertValid('technical failure', 'Evaluation', fixture.failed);
assertValid(
  'replacement revision queued',
  'EvaluationAccepted',
  fixture.revision_queued,
);

const scoreFields = [
  'raw',
  'display',
  'raw_score',
  'display_score',
  'interval',
];
const assertNoScores = (value, context) => {
  for (const field of scoreFields) {
    assert.equal(
      value[field],
      undefined,
      `${context}: non-PASS result contains ${field}`,
    );
  }
};

const assertEvidenceSemantics = (evidence, context) => {
  if (evidence.transcript_span !== undefined) {
    assert.ok(
      evidence.transcript_span.start_utf8_byte <
        evidence.transcript_span.end_utf8_byte,
      `${context}: transcript span must be non-empty`,
    );
  }
  if (evidence.audio_span !== undefined) {
    assert.ok(
      evidence.audio_span.start_ms < evidence.audio_span.end_ms,
      `${context}: audio span must be non-empty`,
    );
  }
};

const assertDimensionSemantics = (dimension, context) => {
  if (dimension.gate_status !== 'PASS') {
    assertNoScores(dimension, context);
    assert.ok(
      dimension.reason_codes.length > 0,
      `${context}: unscored result requires a reason`,
    );
  } else {
    assert.ok(
      dimension.evidence_refs.length > 0,
      `${context}: scored result requires evidence`,
    );
    if (dimension.interval !== undefined) {
      assert.ok(
        dimension.interval[0] <= dimension.interval[1],
        `${context}: interval bounds are reversed`,
      );
    }
  }
  for (const evidence of dimension.evidence_refs) {
    assertEvidenceSemantics(evidence, context);
  }
};

const assertEvaluationSemantics = (evaluation) => {
  const hasSceneChannel = evaluation.channels.includes('SCENE');
  const hasCoreChannel = evaluation.channels.includes('CORE_4D');
  assert.equal(
    evaluation.scene_strategy_ref !== undefined,
    hasSceneChannel,
    `${evaluation.evaluation_id}: SCENE strategy/channel mismatch`,
  );
  assert.equal(
    evaluation.core_4d_strategy_ref !== undefined,
    hasCoreChannel,
    `${evaluation.evaluation_id}: CORE_4D strategy/channel mismatch`,
  );
  if (evaluation.scene_result !== undefined) {
    assert.ok(hasSceneChannel);
    assert.equal(evaluation.scene_result.scene_type, evaluation.scene_type);
    assert.equal(
      evaluation.scene_result.strategy_ref,
      evaluation.scene_strategy_ref,
    );
  }
  if (evaluation.core_4d_observations !== undefined) {
    assert.ok(hasCoreChannel);
    assert.equal(
      new Set(
        evaluation.core_4d_observations.map(
          (observation) => observation.dimension,
        ),
      ).size,
      evaluation.core_4d_observations.length,
      `${evaluation.evaluation_id}: duplicate Core 4D dimensions`,
    );
    assert.ok(
      evaluation.core_4d_observations.every(
        (observation) =>
          observation.strategy_ref === evaluation.core_4d_strategy_ref,
      ),
      `${evaluation.evaluation_id}: Core strategy mismatch`,
    );
  }
  if (evaluation.scoreability_status === 'PROVISIONAL') {
    assert.equal(
      evaluation.is_final,
      false,
      `${evaluation.evaluation_id}: provisional result cannot be final`,
    );
  }
};

for (const evaluation of [
  fixture.core_4d_ready,
  fixture.short_sample_blocked,
  fixture.low_asr_feedback_only,
  fixture.ielts_ready,
  fixture.interview_missing_opportunity,
  fixture.failed,
]) {
  assertEvaluationSemantics(evaluation);
}

const coreDimensions = new Set([
  'PRONUNCIATION',
  'FLUENCY',
  'VOCABULARY',
  'GRAMMAR',
]);
assert.deepEqual(
  new Set(
    fixture.core_4d_ready.core_4d_observations.map(
      (observation) => observation.dimension,
    ),
  ),
  coreDimensions,
);
for (const observation of fixture.core_4d_ready.core_4d_observations) {
  assertDimensionSemantics(
    observation,
    `Core 4D ${observation.dimension}`,
  );
  const weightSum = observation.weights.reduce(
    (sum, weight) => sum + weight.value,
    0,
  );
  assert.ok(
    Math.abs(weightSum - 1) < Number.EPSILON,
    `${observation.dimension}: weights must sum to one`,
  );
  if (observation.dimension === 'PRONUNCIATION') {
    assert.ok(
      observation.evidence_refs.every(
        (evidence) => evidence.audio_span !== undefined,
      ),
      'Pronunciation evidence requires audio spans',
    );
  }
}

for (const evaluation of [
  fixture.short_sample_blocked,
  fixture.low_asr_feedback_only,
]) {
  for (const observation of evaluation.core_4d_observations) {
    assertDimensionSemantics(
      observation,
      `${evaluation.evaluation_id} ${observation.dimension}`,
    );
    assert.equal(observation.profile_update_eligible, false);
  }
}

const ielts = fixture.ielts_ready.scene_result;
const ieltsDimensionIds = new Set(
  ielts.dimensions.map((dimension) => dimension.dimension_id),
);
assert.deepEqual(
  ieltsDimensionIds,
  new Set(['IELTS_FC', 'IELTS_LR', 'IELTS_GRA', 'IELTS_PR']),
);
for (const dimension of ielts.dimensions) {
  assertDimensionSemantics(dimension, dimension.dimension_id);
  if (dimension.dimension_id === 'IELTS_PR') {
    assert.ok(
      dimension.evidence_refs.every(
        (evidence) => evidence.audio_span !== undefined,
      ),
      'IELTS pronunciation requires audio spans',
    );
  }
}
const ieltsOverallRaw =
  ielts.dimensions.reduce((sum, dimension) => sum + dimension.display, 0) /
  ielts.dimensions.length;
const ieltsOverallDisplay = Math.floor(ieltsOverallRaw * 2 + 0.5) / 2;
assert.equal(ieltsOverallRaw, 6.75);
assert.equal(ielts.overall_raw, ieltsOverallRaw);
assert.equal(ielts.overall_display, ieltsOverallDisplay);
assert.equal(ieltsOverallDisplay, 7);

const interview = fixture.interview_missing_opportunity.scene_result;
assert.equal(interview.overall_raw, undefined);
assert.equal(interview.overall_display, undefined);
assert.equal(interview.total_raw, undefined);
assert.equal(interview.total_display, undefined);
assert.equal(interview.weights, undefined);
assert.equal(interview.readiness_level, 'NOT_ASSESSED');
assert.deepEqual(
  new Set(interview.dimensions.map((dimension) => dimension.dimension_id)),
  new Set([
    'INTERVIEW_RELEVANCE',
    'INTERVIEW_STRUCTURE',
    'INTERVIEW_EVIDENCE',
    'INTERVIEW_PROFESSIONAL',
    'INTERVIEW_INTERACTION',
  ]),
);
for (const dimension of interview.dimensions) {
  assertDimensionSemantics(dimension, dimension.dimension_id);
}
const interaction = interview.dimensions.find(
  (dimension) => dimension.dimension_id === 'INTERVIEW_INTERACTION',
);
assert.equal(interaction.gate_status, 'BLOCKED');
assert.deepEqual(interaction.reason_codes, ['OPPORTUNITY_NOT_PROVIDED']);
assert.equal(interview.task_results.length, 1);
assert.equal(interview.question_results.length, 1);
const missingFollowup = interview.question_results[0];
assert.equal(missingFollowup.opportunity_status, 'NOT_PROVIDED');
assert.deepEqual(missingFollowup.response_turn_ids, []);
assert.equal(missingFollowup.dimension_evidence.length, 1);
assert.equal(
  missingFollowup.dimension_evidence[0].dimension_id,
  'INTERVIEW_INTERACTION',
);
assert.equal(
  missingFollowup.dimension_evidence[0].question_score,
  undefined,
);
assert.deepEqual(
  missingFollowup.dimension_evidence[0].reason_codes,
  ['OPPORTUNITY_NOT_PROVIDED'],
);

assert.equal(fixture.failed.scoreability_status, undefined);
assert.equal(fixture.failed.gate_status, undefined);
assert.equal(fixture.failed.scene_result, undefined);
assert.equal(fixture.failed.core_4d_observations, undefined);
assert.ok(fixture.failed.stable_failure);

assert.ok(fixture.revision_queued.revision > 1);
assert.ok(fixture.revision_queued.supersedes_revision_id);

const invalidCreateWithOwner = structuredClone(fixture.create_dual_channel);
invalidCreateWithOwner.owner_user_id = 'user_injected';
assertSchemaRejected(
  'caller-supplied owner',
  'CreateEvaluationRequest',
  invalidCreateWithOwner,
);

const invalidAcceptedEvaluationId = structuredClone(fixture.queued);
invalidAcceptedEvaluationId.evaluation_id = 'evaluation_not_a_uuid';
assertSchemaRejected(
  'non-UUID Evaluation ID',
  'EvaluationAccepted',
  invalidAcceptedEvaluationId,
);

const invalidCreateWithoutSceneStrategy = structuredClone(
  fixture.create_dual_channel,
);
delete invalidCreateWithoutSceneStrategy.scene_strategy_ref;
assertSchemaRejected(
  'SCENE channel without strategy',
  'CreateEvaluationRequest',
  invalidCreateWithoutSceneStrategy,
);

const invalidCreateWithDuplicateChannel = structuredClone(
  fixture.create_dual_channel,
);
invalidCreateWithDuplicateChannel.channels = ['SCENE', 'SCENE'];
assertSchemaRejected(
  'duplicate Evaluation channel',
  'CreateEvaluationRequest',
  invalidCreateWithDuplicateChannel,
);

const invalidBlockedWithScore = structuredClone(
  fixture.short_sample_blocked.core_4d_observations[0],
);
invalidBlockedWithScore.raw_score = 0;
invalidBlockedWithScore.display_score = 0;
assertSchemaRejected(
  'non-PASS Core result with zero score',
  'CoreAbilityObservation',
  invalidBlockedWithScore,
);

const invalidInterviewTotal = structuredClone(interview);
invalidInterviewTotal.overall_raw = 75;
invalidInterviewTotal.overall_display = 75;
assertSchemaRejected(
  'non-IELTS numeric total',
  'SceneEvaluationResult',
  invalidInterviewTotal,
);

const invalidFailedWithGate = structuredClone(fixture.failed);
invalidFailedWithGate.scoreability_status = 'INSUFFICIENT';
invalidFailedWithGate.gate_status = 'BLOCKED';
assertSchemaRejected(
  'technical failure disguised as unscoreable',
  'Evaluation',
  invalidFailedWithGate,
);

const invalidCoreTotal = structuredClone(
  fixture.core_4d_ready.core_4d_observations[0],
);
invalidCoreTotal.total_display = 78;
assertSchemaRejected(
  'Core observation with aggregate total',
  'CoreAbilityObservation',
  invalidCoreTotal,
);

const invalidEvidenceWithoutLocator = structuredClone(
  fixture.core_4d_ready.core_4d_observations[0].evidence_refs[0],
);
delete invalidEvidenceWithoutLocator.audio_span;
assertSchemaRejected(
  'EvidenceRef without a transcript or audio locator',
  'EvidenceRef',
  invalidEvidenceWithoutLocator,
);

const invalidEvidenceWithPermanentUrl = structuredClone(
  fixture.core_4d_ready.core_4d_observations[0].evidence_refs[0],
);
invalidEvidenceWithPermanentUrl.audio_url = 'https://storage.example/audio.wav';
assertSchemaRejected(
  'EvidenceRef exposing a permanent object URL',
  'EvidenceRef',
  invalidEvidenceWithPermanentUrl,
);

const invalidMissingVersion = structuredClone(
  fixture.core_4d_ready.core_4d_observations[0],
);
delete invalidMissingVersion.aggregation_version;
assertSchemaRejected(
  'Core observation missing aggregation version',
  'CoreAbilityObservation',
  invalidMissingVersion,
);

const reversedTranscriptEvidence = structuredClone(
  fixture.core_4d_ready.core_4d_observations[2].evidence_refs[0],
);
reversedTranscriptEvidence.transcript_span.start_utf8_byte = 48;
reversedTranscriptEvidence.transcript_span.end_utf8_byte = 12;
assert.throws(
  () =>
    assertEvidenceSemantics(
      reversedTranscriptEvidence,
      'reversed transcript evidence',
    ),
  /transcript span must be non-empty/,
);

const reversedAudioEvidence = structuredClone(
  fixture.core_4d_ready.core_4d_observations[0].evidence_refs[0],
);
reversedAudioEvidence.audio_span.start_ms = 4800;
reversedAudioEvidence.audio_span.end_ms = 1200;
assert.throws(
  () =>
    assertEvidenceSemantics(reversedAudioEvidence, 'reversed audio evidence'),
  /audio span must be non-empty/,
);

const mismatchedSceneResult = structuredClone(fixture.ielts_ready);
mismatchedSceneResult.scene_result.scene_type = 'INTERVIEW';
assert.throws(
  () => assertEvaluationSemantics(mismatchedSceneResult),
  /Expected values to be strictly equal/,
);

const duplicatedCoreDimension = structuredClone(fixture.core_4d_ready);
duplicatedCoreDimension.core_4d_observations[1].dimension = 'PRONUNCIATION';
assert.throws(
  () => assertEvaluationSemantics(duplicatedCoreDimension),
  /duplicate Core 4D dimensions/,
);

const provisionalMarkedFinal = structuredClone(fixture.low_asr_feedback_only);
provisionalMarkedFinal.is_final = true;
assert.throws(
  () => assertEvaluationSemantics(provisionalMarkedFinal),
  /provisional result cannot be final/,
);
