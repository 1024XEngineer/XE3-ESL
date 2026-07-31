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
    resolve(apiDirectory, 'examples/speech-feedback-contract.json'),
    'utf8',
  ),
);
const componentReferencePrefix = '#/components/schemas/';

const bundleOpenApi = async () => {
  const temporaryDirectory = await mkdtemp(
    resolve(tmpdir(), 'speakup-speech-feedback-bundle-'),
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

const openApi = await bundleOpenApi();
const componentSchemas = openApi.components?.schemas;
assert.ok(componentSchemas, 'The bundled OpenAPI document has no schemas.');

const definitions = {};
const collectSchema = (schemaName) => {
  if (Object.hasOwn(definitions, schemaName)) {
    return;
  }
  const schema = componentSchemas[schemaName];
  assert.ok(schema, `Missing ${schemaName} schema.`);
  definitions[schemaName] = rewriteComponentReferences(schema);
  for (const dependencyName of findComponentReferences(schema)) {
    collectSchema(dependencyName);
  }
};
for (const schemaName of [
  'SpeechFeedback',
  'RetryRequest',
  'RetryTranscriptionCandidate',
  'ConfirmedRetryTurn',
]) {
  collectSchema(schemaName);
}

const ajv = new Ajv2020({
  allErrors: true,
  strict: true,
  strictRequired: false,
});
addFormats(ajv);
ajv.addKeyword({
  keyword: 'discriminator',
  schemaType: 'object',
});
ajv.addKeyword({
  keyword: 'x-max-utf8-bytes',
  schemaType: 'number',
});
ajv.addKeyword({
  keyword: 'x-max-json-bytes',
  schemaType: 'number',
});
ajv.addKeyword({
  keyword: 'x-invariants',
  schemaType: 'array',
});

const compileSchema = (schemaName) =>
  ajv.compile({
    $schema: 'https://json-schema.org/draft/2020-12/schema',
    $ref: `#/$defs/${schemaName}`,
    $defs: definitions,
  });
const validateFeedback = compileSchema('SpeechFeedback');
const validateRetry = compileSchema('RetryRequest');
const validateRetryCandidate = compileSchema('RetryTranscriptionCandidate');
const validateConfirmedRetryTurn = compileSchema('ConfirmedRetryTurn');

const assertValid = (validator, value, label) => {
  if (!validator(value)) {
    const errors = validator.errors
      .map(
        ({ instancePath, keyword, message }) =>
          `${instancePath || '/'} ${keyword}: ${message}`,
      )
      .join('\n');
    assert.fail(`${label} violates its OpenAPI schema:\n${errors}`);
  }
};

for (const name of [
  'queued',
  'running',
  'ready_provisional',
  'ready_provisional_agent',
  'ready_insufficient',
  'failed',
]) {
  assertValid(validateFeedback, fixture[name], name);
  assert.equal(
    fixture[name].status_url,
    `/v1/speech-feedback/${fixture[name].speech_feedback_id}`,
  );
}
for (const name of ['retry_pending', 'retry_turn_created', 'retry_failed']) {
  assertValid(validateRetry, fixture[name], name);
  assert.equal(
    fixture[name].status_url,
    `/v1/retry-requests/${fixture[name].retry_request_id}`,
  );
}
assertValid(
  validateRetryCandidate,
  fixture.retry_transcription_candidate,
  'retry_transcription_candidate',
);
assertValid(
  validateConfirmedRetryTurn,
  fixture.confirmed_retry_turn,
  'confirmed_retry_turn',
);

const assertMutationRejected = (validator, source, label, mutate) => {
  const invalid = structuredClone(source);
  mutate(invalid);
  assert.equal(
    validator(invalid),
    false,
    `${label}: invalid mutation was accepted`,
  );
};

assertMutationRejected(
  validateFeedback,
  fixture.queued,
  'unknown score field',
  (invalid) => {
    invalid.score = 0;
  },
);
assertMutationRejected(
  validateFeedback,
  fixture.queued,
  'queued feedback with a completed item',
  (invalid) => {
    invalid.items = structuredClone(fixture.ready_provisional.items);
  },
);
assertMutationRejected(
  validateFeedback,
  fixture.ready_provisional,
  'provisional feedback without items',
  (invalid) => {
    invalid.items = [];
  },
);
assertMutationRejected(
  validateFeedback,
  fixture.ready_insufficient,
  'insufficient feedback with a technical failure',
  (invalid) => {
    invalid.stable_failure = {
      reason_code: 'INTERNAL_PROCESSING_ERROR',
      retryable: false,
    };
  },
);
assertMutationRejected(
  validateFeedback,
  fixture.failed,
  'technical failure disguised as insufficient evidence',
  (invalid) => {
    invalid.scoreability_status = 'INSUFFICIENT';
    invalid.gate_status = 'BLOCKED';
    invalid.reason_codes = ['TEXT_TOO_SHORT'];
  },
);
assertMutationRejected(
  validateFeedback,
  fixture.running,
  'source union with fields from both branches',
  (invalid) => {
    invalid.source.turn_id = 'turn_cross_source';
  },
);
assertMutationRejected(
  validateFeedback,
  fixture.ready_provisional,
  'anchor union with fields from both branches',
  (invalid) => {
    invalid.items[0].anchor.message_id = 'message_cross_source';
  },
);
assertMutationRejected(
  validateFeedback,
  fixture.ready_provisional,
  'negative UTF-8 byte offset',
  (invalid) => {
    invalid.items[0].anchor.start_utf8_byte = -1;
  },
);
assertMutationRejected(
  validateFeedback,
  fixture.ready_provisional,
  'correction without suggested text',
  (invalid) => {
    delete invalid.items[0].suggested_text;
  },
);
assertMutationRejected(
  validateFeedback,
  fixture.ready_provisional,
  'strength with a suggested correction',
  (invalid) => {
    invalid.items[0].kind = 'STRENGTH';
    invalid.items[0].repractice_mode = 'NONE';
  },
);
assertMutationRejected(
  validateFeedback,
  fixture.ready_provisional,
  'guessed acoustic pronunciation',
  (invalid) => {
    invalid.acoustic_assessment.pronunciation = 'GOOD';
  },
);
assertMutationRejected(
  validateFeedback,
  fixture.ready_provisional,
  'C1 control character in explanation',
  (invalid) => {
    invalid.items[0].explanation = `invalid\u0085text`;
  },
);
assertMutationRejected(
  validateRetry,
  fixture.retry_pending,
  'pending retry with a created Turn',
  (invalid) => {
    invalid.new_turn_id = 'turn_invalid';
  },
);
assertMutationRejected(
  validateRetry,
  fixture.retry_turn_created,
  'created retry with a stable failure',
  (invalid) => {
    invalid.stable_failure = {
      reason_code: 'RETRY_TURN_CREATION_FAILED',
      retryable: true,
    };
  },
);
assertMutationRejected(
  validateRetry,
  fixture.retry_turn_created,
  'created retry without an answer path',
  (invalid) => {
    delete invalid.answer_path;
  },
);
assertMutationRejected(
  validateRetry,
  fixture.retry_failed,
  'failed retry with a created Turn',
  (invalid) => {
    invalid.new_turn_id = 'turn_invalid';
  },
);
assertMutationRejected(
  validateRetryCandidate,
  fixture.retry_transcription_candidate,
  'retry candidate without its draft binding',
  (invalid) => {
    delete invalid.retry_turn_id;
  },
);
assertMutationRejected(
  validateRetryCandidate,
  fixture.retry_transcription_candidate,
  'retry candidate presented as an ordinary candidate',
  (invalid) => {
    invalid.effective_turns = 1;
  },
);
assertMutationRejected(
  validateConfirmedRetryTurn,
  fixture.confirmed_retry_turn,
  'retry confirmation with effective Session progress',
  (invalid) => {
    invalid.effective_turns = 2;
  },
);
assertMutationRejected(
  validateConfirmedRetryTurn,
  fixture.confirmed_retry_turn,
  'retry confirmation counted toward the Turn limit',
  (invalid) => {
    invalid.counts_toward_turn_limit = true;
  },
);

assert.equal(
  fixture.retry_turn_created.answer_path,
  `/v1/retry-turns/${fixture.retry_turn_created.new_turn_id}/transcription-candidates`,
);
assert.equal(fixture.retry_turn_created.new_turn_status, 'ANSWERING');
assert.equal(
  fixture.retry_transcription_candidate.retry_turn_id,
  fixture.retry_turn_created.new_turn_id,
);
assert.equal(
  fixture.retry_transcription_candidate.retry_request_id,
  fixture.retry_turn_created.retry_request_id,
);
assert.equal(
  fixture.retry_transcription_candidate.question_id,
  fixture.retry_turn_created.question_id,
);
assert.equal(
  fixture.confirmed_retry_turn.turn_id,
  fixture.retry_transcription_candidate.retry_turn_id,
);
assert.equal(
  fixture.confirmed_retry_turn.retry_request_id,
  fixture.retry_transcription_candidate.retry_request_id,
);
assert.equal(
  fixture.confirmed_retry_turn.candidate_id,
  fixture.retry_transcription_candidate.candidate_id,
);
assert.equal(
  fixture.confirmed_retry_turn.question_id,
  fixture.retry_transcription_candidate.question_id,
);
assert.equal(
  fixture.confirmed_retry_turn.answer_text,
  fixture.retry_transcription_candidate.transcript,
);
assert.ok(
  Date.parse(fixture.confirmed_retry_turn.confirmed_at) >=
    Date.parse(fixture.confirmed_retry_turn.created_at),
);

const utf8Length = (value) => Buffer.byteLength(value, 'utf8');
for (const feedback of [
  fixture.ready_provisional,
  fixture.ready_provisional_agent,
]) {
  for (const item of feedback.items) {
    assert.equal(
      item.anchor.end_utf8_byte - item.anchor.start_utf8_byte,
      utf8Length(item.anchor.original_excerpt),
      'Fixture anchor offsets must span the exact UTF-8 excerpt.',
    );
    assert.ok(utf8Length(item.anchor.original_excerpt) <= 16384);
    assert.ok(utf8Length(item.explanation) <= 2048);
    if (item.suggested_text !== undefined) {
      assert.ok(utf8Length(item.suggested_text) <= 2048);
    }
    assert.equal(item.speech_feedback_id, feedback.speech_feedback_id);
    if (feedback.source.source_kind === 'CONVERSATION_TURN') {
      assert.equal(item.anchor.anchor_kind, 'CONVERSATION_TRANSCRIPT');
      assert.equal(item.anchor.turn_id, feedback.source.turn_id);
    } else {
      assert.equal(item.anchor.anchor_kind, 'AGENT_TRANSCRIPT');
      assert.equal(item.anchor.message_id, feedback.source.message_id);
      assert.equal(
        item.anchor.transcript_evidence_id,
        feedback.source.transcript_evidence_id,
      );
    }
  }
}

const forbiddenPropertyNames = new Set([
  'score',
  'raw',
  'display',
  'overall',
  'total',
  'weights',
  'profile_update_eligible',
]);
const forbiddenNumericFieldPattern =
  /(^|_)(raw|display|score|overall|total|weight|weights)($|_)/;
const trustedAcousticScoreFields = new Set([
  'accuracy_score',
  'fluency_score',
  'integrity_score',
]);
const collectPropertyNames = (value, names = new Set()) => {
  if (Array.isArray(value)) {
    for (const item of value) {
      collectPropertyNames(item, names);
    }
  } else if (value !== null && typeof value === 'object') {
    for (const [key, item] of Object.entries(value)) {
      if (key === 'properties') {
        for (const propertyName of Object.keys(item)) {
          names.add(propertyName);
        }
      }
      collectPropertyNames(item, names);
    }
  }
  return names;
};
const feedbackPropertyNames = collectPropertyNames(definitions);
for (const fieldName of feedbackPropertyNames) {
  assert.ok(
    !forbiddenPropertyNames.has(fieldName) &&
      (!forbiddenNumericFieldPattern.test(fieldName) ||
        trustedAcousticScoreFields.has(fieldName)),
    `SpeechFeedback must not declare ${fieldName}.`,
  );
}

console.log(
  'Validated six SpeechFeedback examples, three RetryRequest states, one ' +
    'retry candidate, one confirmed retry Turn, strict source/anchor unions, ' +
    'and retry/effective-state isolation.',
);
