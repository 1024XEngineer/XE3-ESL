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
const interviewReportFixture = JSON.parse(
  await readFile(
    resolve(apiDirectory, 'examples/interview-report-contract.json'),
    'utf8',
  ),
);
const ieltsSpeakingReportFixture = JSON.parse(
  await readFile(
    resolve(apiDirectory, 'examples/ielts-speaking-report-contract.json'),
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
    'EvaluationReplay',
    'Evaluation',
    'InterviewReportEnvelope',
    'IeltsSpeakingReportEnvelope',
    'IeltsSpeakingReportIndexPage',
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
const digitLeadingPracticeCreate = structuredClone(
  fixture.create_dual_channel,
);
digitLeadingPracticeCreate.practice_session_id =
  '20000000-0000-4000-8000-000000000001';
assertValid(
  'digit-leading Practice session create',
  'CreateEvaluationRequest',
  digitLeadingPracticeCreate,
);
assertValid('queued create response', 'EvaluationAccepted', fixture.queued);
assert.match(
  fixture.queued.evaluation_id,
  /^[0-9]/,
  'queued fixture must cover a digit-leading Evaluation UUID',
);
assertValid(
  'ready idempotent replay',
  'EvaluationReplay',
  fixture.ready_replay,
);
for (const status of ['RUNNING', 'READY', 'FAILED']) {
  const replay = structuredClone(fixture.ready_replay);
  replay.evaluation_status = status;
  assertValid(
    `${status} idempotent replay`,
    'EvaluationReplay',
    replay,
  );
}
const laterRevisionReplay = structuredClone(fixture.ready_replay);
laterRevisionReplay.evaluation_revision_id =
  'a1000002-0000-4000-8000-000000000002';
laterRevisionReplay.revision = 2;
laterRevisionReplay.supersedes_revision_id =
  'a1000001-0000-4000-8000-000000000001';
assertValid(
  'later current revision idempotent replay',
  'EvaluationReplay',
  laterRevisionReplay,
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

const replayDisguisedAsQueued = structuredClone(fixture.ready_replay);
replayDisguisedAsQueued.evaluation_status = 'QUEUED';
assertSchemaRejected(
  'idempotent replay disguised as queued',
  'EvaluationReplay',
  replayDisguisedAsQueued,
);

const freshAcceptedDisguisedAsReady = structuredClone(fixture.queued);
freshAcceptedDisguisedAsReady.evaluation_status = 'READY';
assertSchemaRejected(
  'fresh accepted response disguised as ready',
  'EvaluationAccepted',
  freshAcceptedDisguisedAsReady,
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

const interviewDimensionOrder = [
  'INTERVIEW_RELEVANCE',
  'INTERVIEW_STRUCTURE',
  'INTERVIEW_EVIDENCE',
  'INTERVIEW_PROFESSIONAL',
  'INTERVIEW_INTERACTION',
];
const interviewFindingKinds = [
  ['strengths', 'strength_finding_ids'],
  ['improvements', 'improvement_finding_ids'],
  ['recommended_expressions', 'recommended_expression_finding_ids'],
];
const forbiddenInterviewReportField = new RegExp(
  String.raw`(^|_)(raw|display|score|overall|total|weight|weights|probe_weight)($|_)`,
  'u',
);

const assertNoInterviewScoreFields = (value, path = 'report') => {
  if (Array.isArray(value)) {
    value.forEach((item, index) =>
      assertNoInterviewScoreFields(item, `${path}[${index}]`),
    );
    return;
  }
  if (value === null || typeof value !== 'object') {
    return;
  }
  for (const [field, item] of Object.entries(value)) {
    assert.doesNotMatch(
      field,
      forbiddenInterviewReportField,
      `${path}.${field} exposes a forbidden numeric score field`,
    );
    assertNoInterviewScoreFields(item, `${path}.${field}`);
  }
};

const sortedStrings = (values) => [...values].sort();

const assertInterviewReportSemantics = (envelope) => {
  assert.equal(
    envelope.status_url,
    `/v1/practice-sessions/${envelope.practice_session_id}/interview-report`,
    'Interview report status_url must address the same Practice Session',
  );
  if (envelope.evaluation_status !== 'READY') {
    return;
  }

  const report = envelope.report;
  assert.equal(report.schema_version, 'interview-report/v1');
  assert.equal(report.readiness_level, 'NOT_ASSESSED');
  assertNoInterviewScoreFields(report);
  assert.deepEqual(
    report.dimensions.map((dimension) => dimension.dimension_id),
    interviewDimensionOrder,
    'Interview report dimensions must use the canonical order',
  );

  const questionById = new Map();
  const questionByTurnId = new Map();
  for (const question of report.questions) {
    assert.ok(
      !questionById.has(question.question_id),
      `Duplicate question ${question.question_id}`,
    );
    questionById.set(question.question_id, question);
    if (question.response_turn_id !== undefined) {
      assert.ok(
        !questionByTurnId.has(question.response_turn_id),
        `Duplicate response Turn ${question.response_turn_id}`,
      );
      questionByTurnId.set(question.response_turn_id, question);
    }
    assert.deepEqual(
      question.dimension_findings.map(
        (dimension) => dimension.dimension_id,
      ),
      interviewDimensionOrder,
      `${question.question_id}: dimension finding order changed`,
    );
    if (question.assessment_status === 'NOT_ASSESSED') {
      for (const dimension of question.dimension_findings) {
        for (const [, referenceField] of interviewFindingKinds) {
          assert.deepEqual(
            dimension[referenceField],
            [],
            `${question.question_id}: unassessed question references findings`,
          );
        }
      }
    }
  }
  for (const question of report.questions) {
    if (question.question_type === 'FOLLOW_UP') {
      const parent = questionById.get(question.parent_question_id);
      assert.ok(parent, `${question.question_id}: parent question is missing`);
      assert.equal(
        parent.question_type,
        'PRIMARY',
        `${question.question_id}: parent must be PRIMARY`,
      );
    }
  }

  const findingById = new Map();
  for (const dimension of report.dimensions) {
    if (report.scoreability_status === 'INSUFFICIENT') {
      assert.equal(dimension.scoreability_status, 'INSUFFICIENT');
      assert.equal(dimension.gate_status, 'BLOCKED');
    } else {
      assert.ok(
        (dimension.scoreability_status === 'PROVISIONAL' &&
          dimension.gate_status === 'FEEDBACK_ONLY') ||
          (dimension.scoreability_status === 'INSUFFICIENT' &&
            dimension.gate_status === 'BLOCKED'),
        `${dimension.dimension_id}: invalid qualitative gate`,
      );
    }

    const evidenceRefIds = new Set();
    for (const [findingField, referenceField] of interviewFindingKinds) {
      for (const finding of dimension[findingField]) {
        assert.ok(
          !findingById.has(finding.finding_id),
          `Duplicate finding ${finding.finding_id}`,
        );
        findingById.set(finding.finding_id, {
          dimensionId: dimension.dimension_id,
          findingField,
          referenceField,
          finding,
        });
        for (const evidence of finding.evidence) {
          evidenceRefIds.add(evidence.evidence_ref_id);
          assert.ok(
            evidence.start_utf8_byte < evidence.end_utf8_byte,
            `${finding.finding_id}: evidence span must be non-empty`,
          );
          const question = questionByTurnId.get(evidence.turn_id);
          assert.ok(
            question,
            `${finding.finding_id}: evidence Turn is not a confirmed response`,
          );
          assert.ok(
            question.evidence_ref_ids.includes(evidence.evidence_ref_id),
            `${finding.finding_id}: evidence reference is outside its response`,
          );
          const transcriptBytes = Buffer.from(
            question.confirmed_transcript,
            'utf8',
          );
          assert.ok(
            evidence.end_utf8_byte <= transcriptBytes.length,
            `${finding.finding_id}: evidence span exceeds its response`,
          );
          assert.equal(
            transcriptBytes
              .subarray(evidence.start_utf8_byte, evidence.end_utf8_byte)
              .toString('utf8'),
            evidence.original_excerpt,
            `${finding.finding_id}: evidence excerpt does not match its response`,
          );
        }
      }
    }
    assert.deepEqual(
      sortedStrings(dimension.evidence_ref_ids),
      sortedStrings(evidenceRefIds),
      `${dimension.dimension_id}: evidence_ref_ids must equal finding evidence`,
    );
  }

  for (const question of report.questions) {
    for (const dimension of question.dimension_findings) {
      for (const [, referenceField] of interviewFindingKinds) {
        for (const findingId of dimension[referenceField]) {
          const target = findingById.get(findingId);
          assert.ok(
            target,
            `${question.question_id}: dangling finding ${findingId}`,
          );
          assert.equal(
            target.dimensionId,
            dimension.dimension_id,
            `${question.question_id}: finding dimension mismatch`,
          );
          assert.equal(
            target.referenceField,
            referenceField,
            `${question.question_id}: finding category mismatch`,
          );
          assert.ok(
            target.finding.evidence.some((evidence) =>
              question.evidence_ref_ids.includes(evidence.evidence_ref_id),
            ),
            `${question.question_id}: finding is unrelated to its response`,
          );
        }
      }
    }
  }

  for (const action of report.priority_actions) {
    const target = findingById.get(action.finding_id);
    assert.ok(target, `Priority action ${action.finding_id} is dangling`);
    assert.equal(
      target.dimensionId,
      action.dimension_id,
      `Priority action ${action.finding_id} has the wrong dimension`,
    );
    assert.equal(
      target.findingField,
      'improvements',
      `Priority action ${action.finding_id} must reference an improvement`,
    );
  }
};

for (const [name, value] of Object.entries(interviewReportFixture)) {
  assertValid(
    `Interview report ${name}`,
    'InterviewReportEnvelope',
    value,
  );
  assertInterviewReportSemantics(value);
}

const digitLeadingInterviewReport = structuredClone(
  interviewReportFixture.ready,
);
digitLeadingInterviewReport.practice_session_id =
  '20000000-0000-4000-8000-000000000001';
digitLeadingInterviewReport.status_url =
  '/v1/practice-sessions/20000000-0000-4000-8000-000000000001/interview-report';
assertValid(
  'digit-leading Practice session Interview report',
  'InterviewReportEnvelope',
  digitLeadingInterviewReport,
);
assertInterviewReportSemantics(digitLeadingInterviewReport);

for (const reasonCode of [
  'POLICY_VIOLATION',
  'EVIDENCE_REF_INVALID',
  'VERSION_CONFLICT',
]) {
  const nonRetryableFailure = structuredClone(interviewReportFixture.failed);
  nonRetryableFailure.stable_failure = {
    reason_code: reasonCode,
    retryable: false,
  };
  assertValid(
    `Interview report ${reasonCode} failure`,
    'InterviewReportEnvelope',
    nonRetryableFailure,
  );
}

const retryablePolicyFailure = structuredClone(interviewReportFixture.failed);
retryablePolicyFailure.stable_failure = {
  reason_code: 'POLICY_VIOLATION',
  retryable: true,
};
assertSchemaRejected(
  'retryable non-transient Interview report failure',
  'InterviewReportEnvelope',
  retryablePolicyFailure,
);

const nonRetryableInternalFailure = structuredClone(
  interviewReportFixture.failed,
);
nonRetryableInternalFailure.stable_failure.retryable = false;
assertSchemaRejected(
  'non-retryable INTERNAL_RETRYABLE Interview report failure',
  'InterviewReportEnvelope',
  nonRetryableInternalFailure,
);

const readyWithoutReport = structuredClone(interviewReportFixture.ready);
delete readyWithoutReport.report;
assertSchemaRejected(
  'READY Interview report without report',
  'InterviewReportEnvelope',
  readyWithoutReport,
);

const failedWithReport = structuredClone(interviewReportFixture.failed);
failedWithReport.report = structuredClone(interviewReportFixture.ready.report);
assertSchemaRejected(
  'FAILED Interview report carrying a report',
  'InterviewReportEnvelope',
  failedWithReport,
);

const pendingWithReport = structuredClone(interviewReportFixture.running);
pendingWithReport.report = structuredClone(interviewReportFixture.ready.report);
assertSchemaRejected(
  'pending Interview report carrying a report',
  'InterviewReportEnvelope',
  pendingWithReport,
);

const reportWithUnknownVersion = structuredClone(interviewReportFixture.ready);
reportWithUnknownVersion.report.schema_version = 'interview-report/v2';
assertSchemaRejected(
  'Interview report with unknown schema version',
  'InterviewReportEnvelope',
  reportWithUnknownVersion,
);

for (const [caseName, mutate] of [
  ['overall score', (value) => (value.report.overall = 75)],
  ['dimension raw value', (value) => (value.report.dimensions[0].raw = 75)],
  [
    'finding display score',
    (value) =>
      (value.report.dimensions[0].improvements[0].display_score = 75),
  ],
  [
    'question probe weight',
    (value) => (value.report.questions[0].probe_weight = 1),
  ],
  [
    'priority action weight',
    (value) => (value.report.priority_actions[0].weight = 1),
  ],
]) {
  const invalid = structuredClone(interviewReportFixture.ready);
  mutate(invalid);
  assertSchemaRejected(
    `Interview report with ${caseName}`,
    'InterviewReportEnvelope',
    invalid,
  );
}

const unknownReportReason = structuredClone(interviewReportFixture.ready);
unknownReportReason.report.dimensions[0].reason_codes = ['UNKNOWN'];
assertSchemaRejected(
  'Interview report with free-text reason relationship',
  'InterviewReportEnvelope',
  unknownReportReason,
);

const unknownQuestionType = structuredClone(interviewReportFixture.ready);
unknownQuestionType.report.questions[0].question_type = 'RELATED';
assertSchemaRejected(
  'Interview report with free-text question relationship',
  'InterviewReportEnvelope',
  unknownQuestionType,
);

const unknownParent = structuredClone(interviewReportFixture.ready);
unknownParent.report.questions[1].parent_question_id = 'question_missing';
assert.throws(
  () => assertInterviewReportSemantics(unknownParent),
  /parent question is missing/,
);

const danglingQuestionFinding = structuredClone(
  interviewReportFixture.ready,
);
danglingQuestionFinding.report.questions[0].dimension_findings[0]
  .improvement_finding_ids = ['interview_finding_missing'];
assertValid(
  'schema-valid dangling question finding',
  'InterviewReportEnvelope',
  danglingQuestionFinding,
);
assert.throws(
  () => assertInterviewReportSemantics(danglingQuestionFinding),
  /dangling finding/,
);

const danglingPriorityAction = structuredClone(
  interviewReportFixture.ready,
);
danglingPriorityAction.report.priority_actions[0].finding_id =
  'interview_finding_missing';
assertValid(
  'schema-valid dangling priority action',
  'InterviewReportEnvelope',
  danglingPriorityAction,
);
assert.throws(
  () => assertInterviewReportSemantics(danglingPriorityAction),
  /is dangling/,
);

const mismatchedFindingCategory = structuredClone(
  interviewReportFixture.ready,
);
mismatchedFindingCategory.report.questions[0].dimension_findings[0]
  .strength_finding_ids = ['interview_finding_relevance_001'];
mismatchedFindingCategory.report.questions[0].dimension_findings[0]
  .improvement_finding_ids = [];
assertValid(
  'schema-valid mismatched finding category',
  'InterviewReportEnvelope',
  mismatchedFindingCategory,
);
assert.throws(
  () => assertInterviewReportSemantics(mismatchedFindingCategory),
  /finding category mismatch/,
);

const forgedInterviewExcerpt = structuredClone(interviewReportFixture.ready);
forgedInterviewExcerpt.report.dimensions[0].improvements[0].evidence[0]
  .original_excerpt = 'forged excerpt';
assertValid(
  'schema-valid forged Interview excerpt',
  'InterviewReportEnvelope',
  forgedInterviewExcerpt,
);
assert.throws(
  () => assertInterviewReportSemantics(forgedInterviewExcerpt),
  /evidence excerpt does not match/,
);

const mismatchedReportStatusUrl = structuredClone(
  interviewReportFixture.running,
);
mismatchedReportStatusUrl.status_url =
  '/v1/practice-sessions/session_other/interview-report';
assertValid(
  'schema-valid mismatched Interview status URL',
  'InterviewReportEnvelope',
  mismatchedReportStatusUrl,
);
assert.throws(
  () => assertInterviewReportSemantics(mismatchedReportStatusUrl),
  /same Practice Session/,
);

const ieltsCriterionOrder = ['IELTS_FC', 'IELTS_LR', 'IELTS_GRA', 'IELTS_PR'];
const ieltsPartOrder = ['PART_1', 'PART_2', 'PART_3'];
const ieltsPartQuestionIndexes = [
  [1, 2, 3, 4, 5, 6, 7, 8],
  [9],
  [10, 11, 12, 13, 14],
];
const ieltsFindingKinds = [
  ['strengths', 'strength_finding_ids'],
  ['improvements', 'improvement_finding_ids'],
  ['upgrade_examples', 'upgrade_example_finding_ids'],
];

const expectedIeltsPart = (index) => {
  if (index <= 8) {
    return 'PART_1';
  }
  if (index === 9) {
    return 'PART_2';
  }
  return 'PART_3';
};

const assertIeltsSpeakingReportSemantics = (envelope) => {
  assert.equal(
    envelope.status_url,
    `/v1/practice-sessions/${envelope.practice_session_id}/ielts-speaking-report`,
    'IELTS report status_url must address the same Practice Session',
  );
  if (envelope.evaluation_status !== 'READY') {
    return;
  }

  const report = envelope.report;
  assert.equal(report.schema_version, 'ielts-speaking-report/v1');
  assert.equal(
    report.disclaimer_code,
    'AI_PRACTICE_ESTIMATE_NOT_OFFICIAL_IELTS',
  );
  assert.equal(report.speaking_overall.status, 'NOT_AVAILABLE');
  assert.equal(report.target_plan.status, 'NOT_CONFIGURED');
  assert.deepEqual(
    report.criteria.map((criterion) => criterion.criterion_id),
    ieltsCriterionOrder,
    'IELTS criteria must use the canonical order',
  );
  assert.deepEqual(
    report.part_reviews.map((part) => part.part_id),
    ieltsPartOrder,
    'IELTS Parts must use the canonical order',
  );

  const questionByIndex = new Map();
  const questionByTurnId = new Map();
  const questionByEvidenceRefId = new Map();
  const questionIds = new Set();
  for (let offset = 0; offset < report.questions.length; offset += 1) {
    const question = report.questions[offset];
    const expectedIndex = offset + 1;
    assert.equal(
      question.index,
      expectedIndex,
      'IELTS questions must use the frozen sequence 1..14',
    );
    assert.equal(
      question.part_id,
      expectedIeltsPart(expectedIndex),
      `Question ${expectedIndex} has the wrong IELTS Part`,
    );
    assert.ok(
      !questionIds.has(question.question_id),
      `Duplicate question ${question.question_id}`,
    );
    questionIds.add(question.question_id);
    questionByIndex.set(question.index, question);
    if (question.response_turn_id !== undefined) {
      assert.ok(
        !questionByTurnId.has(question.response_turn_id),
        `Duplicate response Turn ${question.response_turn_id}`,
      );
      questionByTurnId.set(question.response_turn_id, question);
    }
    for (const evidenceRefId of question.evidence_ref_ids) {
      assert.ok(
        !questionByEvidenceRefId.has(evidenceRefId),
        `Duplicate response EvidenceRef ${evidenceRefId}`,
      );
      questionByEvidenceRefId.set(evidenceRefId, question);
    }
    assert.deepEqual(
      question.criterion_findings.map(
        (criterion) => criterion.criterion_id,
      ),
      ieltsCriterionOrder,
      `Question ${expectedIndex}: criterion finding order changed`,
    );
    if (question.assessment_status === 'NOT_ASSESSED') {
      for (const criterion of question.criterion_findings) {
        for (const [, referenceField] of ieltsFindingKinds) {
          assert.deepEqual(
            criterion[referenceField],
            [],
            `Question ${expectedIndex}: unassessed response references findings`,
          );
        }
      }
    }
  }

  const findingById = new Map();
  for (const criterion of report.criteria) {
    if (criterion.criterion_id === 'IELTS_FC') {
      assert.equal(criterion.estimated_band, undefined);
      assert.equal(criterion.band_descriptor, undefined);
    }
    if (criterion.criterion_id === 'IELTS_PR') {
      assert.equal(criterion.scoreability_status, 'INSUFFICIENT');
      assert.equal(criterion.gate_status, 'BLOCKED');
      assert.equal(criterion.estimated_band, undefined);
      assert.equal(criterion.band_descriptor, undefined);
      assert.deepEqual(criterion.strengths, []);
      assert.deepEqual(criterion.improvements, []);
      assert.deepEqual(criterion.upgrade_examples, []);
    }
    if (
      ['IELTS_LR', 'IELTS_GRA'].includes(criterion.criterion_id) &&
      criterion.scoreability_status === 'PROVISIONAL'
    ) {
      assert.ok(
        Number.isInteger(criterion.estimated_band) &&
          criterion.estimated_band >= 1 &&
          criterion.estimated_band <= 9,
        `${criterion.criterion_id}: provisional Band must be an integer 1..9`,
      );
      assert.equal(typeof criterion.band_descriptor, 'string');
    }
    if (criterion.scoreability_status === 'INSUFFICIENT') {
      assert.equal(criterion.estimated_band, undefined);
      assert.equal(criterion.band_descriptor, undefined);
    }

    const evidenceRefIds = new Set();
    for (const [findingField, referenceField] of ieltsFindingKinds) {
      for (const finding of criterion[findingField]) {
        assert.ok(
          !findingById.has(finding.finding_id),
          `Duplicate IELTS finding ${finding.finding_id}`,
        );
        findingById.set(finding.finding_id, {
          criterionId: criterion.criterion_id,
          findingField,
          referenceField,
          finding,
        });
        for (const evidence of finding.evidence) {
          evidenceRefIds.add(evidence.evidence_ref_id);
          assert.ok(
            evidence.start_utf8_byte < evidence.end_utf8_byte,
            `${finding.finding_id}: evidence span must be non-empty`,
          );
          const question = questionByTurnId.get(evidence.turn_id);
          assert.ok(
            question,
            `${finding.finding_id}: evidence Turn is not a confirmed response`,
          );
          assert.ok(
            question.evidence_ref_ids.includes(evidence.evidence_ref_id),
            `${finding.finding_id}: evidence reference is outside its response`,
          );
          const transcriptBytes = Buffer.from(
            question.confirmed_transcript,
            'utf8',
          );
          assert.ok(
            evidence.end_utf8_byte <= transcriptBytes.length,
            `${finding.finding_id}: evidence span exceeds its response`,
          );
          assert.equal(
            transcriptBytes
              .subarray(evidence.start_utf8_byte, evidence.end_utf8_byte)
              .toString('utf8'),
            evidence.original_excerpt,
            `${finding.finding_id}: evidence excerpt does not match its response`,
          );
        }
      }
    }
    assert.deepEqual(
      sortedStrings(criterion.evidence_ref_ids),
      sortedStrings(evidenceRefIds),
      `${criterion.criterion_id}: evidence_ref_ids must equal finding evidence`,
    );
  }

  for (const question of report.questions) {
    for (const criterion of question.criterion_findings) {
      for (const [, referenceField] of ieltsFindingKinds) {
        for (const findingId of criterion[referenceField]) {
          const target = findingById.get(findingId);
          assert.ok(
            target,
            `Question ${question.index}: dangling finding ${findingId}`,
          );
          assert.equal(
            target.criterionId,
            criterion.criterion_id,
            `Question ${question.index}: finding criterion mismatch`,
          );
          assert.equal(
            target.referenceField,
            referenceField,
            `Question ${question.index}: finding category mismatch`,
          );
          assert.ok(
            target.finding.evidence.some((evidence) =>
              question.evidence_ref_ids.includes(evidence.evidence_ref_id),
            ),
            `Question ${question.index}: finding is unrelated to its response`,
          );
        }
      }
    }
  }

  for (let offset = 0; offset < report.part_reviews.length; offset += 1) {
    const part = report.part_reviews[offset];
    const expectedIndexes = ieltsPartQuestionIndexes[offset];
    assert.deepEqual(
      part.question_indexes,
      expectedIndexes,
      `${part.part_id}: question indexes changed`,
    );
    const partEvidence = new Set(
      expectedIndexes.flatMap(
        (index) => questionByIndex.get(index).evidence_ref_ids,
      ),
    );
    assert.deepEqual(
      sortedStrings(part.evidence_ref_ids),
      sortedStrings(partEvidence),
      `${part.part_id}: evidence refs must equal its question evidence`,
    );
    for (const [, referenceField] of ieltsFindingKinds) {
      for (const findingId of part[referenceField]) {
        const target = findingById.get(findingId);
        assert.ok(target, `${part.part_id}: dangling finding ${findingId}`);
        assert.equal(
          target.referenceField,
          referenceField,
          `${part.part_id}: finding category mismatch`,
        );
        assert.ok(
          target.finding.evidence.some((evidence) =>
            partEvidence.has(evidence.evidence_ref_id),
          ),
          `${part.part_id}: finding is unrelated to the Part`,
        );
      }
    }
  }

  for (const action of report.priority_actions) {
    const target = findingById.get(action.finding_id);
    assert.ok(target, `Priority action ${action.finding_id} is dangling`);
    assert.equal(
      target.criterionId,
      action.criterion_id,
      `Priority action ${action.finding_id} has the wrong criterion`,
    );
    assert.equal(
      target.findingField,
      'improvements',
      `Priority action ${action.finding_id} must reference an improvement`,
    );
  }

  if (report.scoreability_status === 'INSUFFICIENT') {
    assert.ok(
      report.criteria.every(
        (criterion) =>
          criterion.scoreability_status === 'INSUFFICIENT' &&
          criterion.gate_status === 'BLOCKED',
      ),
      'INSUFFICIENT report cannot retain provisional criteria',
    );
    assert.deepEqual(report.priority_actions, []);
  }
};

const assertIeltsSpeakingIndexSemantics = (page) => {
  const practiceSessions = new Set();
  const evaluationIds = new Set();
  for (const item of page.items) {
    assert.ok(
      !practiceSessions.has(item.practice_session_id),
      `Duplicate IELTS report Practice Session ${item.practice_session_id}`,
    );
    practiceSessions.add(item.practice_session_id);
    assert.ok(
      !evaluationIds.has(item.evaluation_id),
      `Duplicate IELTS report Evaluation ${item.evaluation_id}`,
    );
    evaluationIds.add(item.evaluation_id);
    assert.equal(
      item.status_url,
      `/v1/practice-sessions/${item.practice_session_id}/ielts-speaking-report`,
      'IELTS index status_url must address its Practice Session',
    );
    assert.ok(
      Date.parse(item.created_at) <= Date.parse(item.updated_at),
      'IELTS index updated_at precedes created_at',
    );
  }
};

for (const [name, value] of Object.entries(ieltsSpeakingReportFixture)) {
  if (name === 'index_page') {
    continue;
  }
  assertValid(
    `IELTS Speaking report ${name}`,
    'IeltsSpeakingReportEnvelope',
    value,
  );
  assertIeltsSpeakingReportSemantics(value);
}
const digitLeadingIeltsReport = structuredClone(
  ieltsSpeakingReportFixture.ready,
);
digitLeadingIeltsReport.practice_session_id =
  '20000000-0000-4000-8000-000000000001';
digitLeadingIeltsReport.status_url =
  '/v1/practice-sessions/20000000-0000-4000-8000-000000000001/ielts-speaking-report';
assertValid(
  'digit-leading Practice session IELTS Speaking report',
  'IeltsSpeakingReportEnvelope',
  digitLeadingIeltsReport,
);
assertIeltsSpeakingReportSemantics(digitLeadingIeltsReport);

const ieltsReportWithMissingOpportunity = structuredClone(
  ieltsSpeakingReportFixture.ready,
);
const missingQuestion =
  ieltsReportWithMissingOpportunity.report.questions[13];
missingQuestion.opportunity_status = 'NOT_PROVIDED';
missingQuestion.assessment_status = 'NOT_ASSESSED';
delete missingQuestion.confirmed_transcript;
delete missingQuestion.response_turn_id;
missingQuestion.evidence_ref_ids = [];
ieltsReportWithMissingOpportunity.report.part_reviews[2]
  .evidence_ref_ids.pop();
assertValid(
  'IELTS Speaking report with a missing opportunity',
  'IeltsSpeakingReportEnvelope',
  ieltsReportWithMissingOpportunity,
);
assertIeltsSpeakingReportSemantics(ieltsReportWithMissingOpportunity);

assertValid(
  'IELTS Speaking report index',
  'IeltsSpeakingReportIndexPage',
  ieltsSpeakingReportFixture.index_page,
);
assertIeltsSpeakingIndexSemantics(ieltsSpeakingReportFixture.index_page);
const digitLeadingIeltsReportIndex = structuredClone(
  ieltsSpeakingReportFixture.index_page,
);
digitLeadingIeltsReportIndex.items[0].practice_session_id =
  '20000000-0000-4000-8000-000000000001';
digitLeadingIeltsReportIndex.items[0].status_url =
  '/v1/practice-sessions/20000000-0000-4000-8000-000000000001/ielts-speaking-report';
assertValid(
  'digit-leading Practice session IELTS Speaking report index',
  'IeltsSpeakingReportIndexPage',
  digitLeadingIeltsReportIndex,
);
assertIeltsSpeakingIndexSemantics(digitLeadingIeltsReportIndex);

for (const reasonCode of [
  'POLICY_VIOLATION',
  'EVIDENCE_REF_INVALID',
  'VERSION_CONFLICT',
  'INTERNAL_NON_RETRYABLE',
]) {
  const failure = structuredClone(ieltsSpeakingReportFixture.failed);
  failure.stable_failure = { reason_code: reasonCode, retryable: false };
  assertValid(
    `IELTS Speaking ${reasonCode} failure`,
    'IeltsSpeakingReportEnvelope',
    failure,
  );
}
const retryableNonTransientIeltsFailure = structuredClone(
  ieltsSpeakingReportFixture.failed,
);
retryableNonTransientIeltsFailure.stable_failure = {
  reason_code: 'INTERNAL_NON_RETRYABLE',
  retryable: true,
};
assertSchemaRejected(
  'retryable non-transient IELTS Speaking failure',
  'IeltsSpeakingReportEnvelope',
  retryableNonTransientIeltsFailure,
);

const ieltsReadyWithoutReport = structuredClone(
  ieltsSpeakingReportFixture.ready,
);
delete ieltsReadyWithoutReport.report;
assertSchemaRejected(
  'READY IELTS Speaking report without report',
  'IeltsSpeakingReportEnvelope',
  ieltsReadyWithoutReport,
);

for (const [caseName, mutate] of [
  [
    'FC Band',
    (value) => {
      value.report.criteria[0].estimated_band = 6;
      value.report.criteria[0].band_descriptor = 'forbidden';
    },
  ],
  [
    'PR Band',
    (value) => {
      value.report.criteria[3].estimated_band = 6;
      value.report.criteria[3].band_descriptor = 'forbidden';
    },
  ],
  [
    'numeric Speaking Overall',
    (value) => (value.report.speaking_overall.estimated_band = 6),
  ],
  [
    'Part Band',
    (value) => (value.report.part_reviews[0].estimated_band = 6),
  ],
  [
    'question score',
    (value) => (value.report.questions[0].score = 6),
  ],
  [
    'invented target Band',
    (value) => (value.report.target_plan.target_band = 7),
  ],
]) {
  const invalid = structuredClone(ieltsSpeakingReportFixture.ready);
  mutate(invalid);
  assertSchemaRejected(
    `IELTS Speaking report with ${caseName}`,
    'IeltsSpeakingReportEnvelope',
    invalid,
  );
}

const wrongIeltsQuestionIndex = structuredClone(
  ieltsSpeakingReportFixture.ready,
);
wrongIeltsQuestionIndex.report.questions[0].index = 2;
assertValid(
  'schema-valid wrong IELTS question index',
  'IeltsSpeakingReportEnvelope',
  wrongIeltsQuestionIndex,
);
assert.throws(
  () => assertIeltsSpeakingReportSemantics(wrongIeltsQuestionIndex),
  /frozen sequence/,
);

const wrongIeltsPartIndexes = structuredClone(
  ieltsSpeakingReportFixture.ready,
);
wrongIeltsPartIndexes.report.part_reviews[0].question_indexes = [1, 2, 3];
assertValid(
  'schema-valid wrong IELTS Part indexes',
  'IeltsSpeakingReportEnvelope',
  wrongIeltsPartIndexes,
);
assert.throws(
  () => assertIeltsSpeakingReportSemantics(wrongIeltsPartIndexes),
  /question indexes changed/,
);

const forgedIeltsExcerpt = structuredClone(ieltsSpeakingReportFixture.ready);
forgedIeltsExcerpt.report.criteria[0].strengths[0].evidence[0]
  .original_excerpt = 'forged excerpt';
assertValid(
  'schema-valid forged IELTS excerpt',
  'IeltsSpeakingReportEnvelope',
  forgedIeltsExcerpt,
);
assert.throws(
  () => assertIeltsSpeakingReportSemantics(forgedIeltsExcerpt),
  /evidence excerpt does not match/,
);

const danglingIeltsQuestionFinding = structuredClone(
  ieltsSpeakingReportFixture.ready,
);
danglingIeltsQuestionFinding.report.questions[0].criterion_findings[1]
  .improvement_finding_ids = ['ielts_finding_missing'];
assertValid(
  'schema-valid dangling IELTS question finding',
  'IeltsSpeakingReportEnvelope',
  danglingIeltsQuestionFinding,
);
assert.throws(
  () => assertIeltsSpeakingReportSemantics(danglingIeltsQuestionFinding),
  /dangling finding/,
);

const duplicatedIeltsQuestionEvidence = structuredClone(
  ieltsSpeakingReportFixture.ready,
);
duplicatedIeltsQuestionEvidence.report.questions[2].evidence_ref_ids = [
  duplicatedIeltsQuestionEvidence.report.questions[1].evidence_ref_ids[0],
];
duplicatedIeltsQuestionEvidence.report.part_reviews[0].evidence_ref_ids =
  duplicatedIeltsQuestionEvidence.report.part_reviews[0].evidence_ref_ids
    .filter((refID) => refID !== 'ielts_evidence_003');
assertValid(
  'schema-valid duplicate IELTS question EvidenceRef',
  'IeltsSpeakingReportEnvelope',
  duplicatedIeltsQuestionEvidence,
);
assert.throws(
  () => assertIeltsSpeakingReportSemantics(duplicatedIeltsQuestionEvidence),
  /Duplicate response EvidenceRef/,
);

const mismatchedIeltsStatusUrl = structuredClone(
  ieltsSpeakingReportFixture.running,
);
mismatchedIeltsStatusUrl.status_url =
  '/v1/practice-sessions/session_other/ielts-speaking-report';
assertValid(
  'schema-valid mismatched IELTS status URL',
  'IeltsSpeakingReportEnvelope',
  mismatchedIeltsStatusUrl,
);
assert.throws(
  () => assertIeltsSpeakingReportSemantics(mismatchedIeltsStatusUrl),
  /same Practice Session/,
);

const duplicateIeltsIndexItem = structuredClone(
  ieltsSpeakingReportFixture.index_page,
);
duplicateIeltsIndexItem.items.push(
  structuredClone(duplicateIeltsIndexItem.items[0]),
);
assertValid(
  'schema-valid duplicate IELTS index item',
  'IeltsSpeakingReportIndexPage',
  duplicateIeltsIndexItem,
);
assert.throws(
  () => assertIeltsSpeakingIndexSemantics(duplicateIeltsIndexItem),
  /Duplicate IELTS report Practice Session/,
);
