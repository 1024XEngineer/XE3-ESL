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
  await readFile(resolve(apiDirectory, 'examples/mock-main-flow.json'), 'utf8'),
);

const componentReferencePrefix = '#/components/schemas/';
const formalDtoNames = [
  'PracticeSession',
  'PracticeParticipant',
  'PracticeSessionPolicy',
  'Question',
  'Turn',
  'CreateRetryTurnCommand',
  'CreateRetryTurnResult',
  'TurnAnalysis',
  'FeedbackItem',
  'RetryRequest',
  'HistoryRecord',
];

const bundleOpenApi = async () => {
  const temporaryDirectory = await mkdtemp(
    resolve(tmpdir(), 'speakup-openapi-bundle-'),
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

const openApiBundle = await bundleOpenApi();
const componentSchemas = openApiBundle.components?.schemas;
if (componentSchemas === undefined) {
  throw new Error('The bundled OpenAPI document has no component schemas.');
}

const formalDefinitions = {};
const collectFormalSchema = (schemaName) => {
  if (Object.hasOwn(formalDefinitions, schemaName)) {
    return;
  }
  const schema = componentSchemas[schemaName];
  if (schema === undefined) {
    throw new Error(`The bundled OpenAPI document has no ${schemaName} schema.`);
  }

  formalDefinitions[schemaName] = rewriteComponentReferences(schema);
  for (const dependencyName of findComponentReferences(schema)) {
    collectFormalSchema(dependencyName);
  }
};
for (const schemaName of formalDtoNames) {
  collectFormalSchema(schemaName);
}

const fixtureSchema = {
  $schema: 'https://json-schema.org/draft/2020-12/schema',
  type: 'object',
  additionalProperties: false,
  required: [
    'fixture_id',
    'practice_session',
    'session_policy',
    'participants',
    'questions',
    'effective_turns',
    'turn_analyses',
    'feedback_items',
    'retry_request_pending',
    'create_retry_turn_command',
    'create_retry_turn_results',
    'retry_turn_created',
    'retry_request',
    'retry_turn',
    'retry_progress_decision',
    'history_records',
  ],
  properties: {
    fixture_id: {
      type: 'string',
      minLength: 1,
    },
    practice_session: {
      $ref: '#/$defs/PracticeSession',
    },
    session_policy: {
      $ref: '#/$defs/PracticeSessionPolicy',
    },
    participants: {
      type: 'array',
      items: {
        $ref: '#/$defs/PracticeParticipant',
      },
    },
    questions: {
      type: 'array',
      items: {
        $ref: '#/$defs/Question',
      },
    },
    effective_turns: {
      type: 'array',
      items: {
        $ref: '#/$defs/Turn',
      },
    },
    turn_analyses: {
      type: 'array',
      items: {
        $ref: '#/$defs/TurnAnalysis',
      },
    },
    feedback_items: {
      type: 'array',
      items: {
        $ref: '#/$defs/FeedbackItem',
      },
    },
    retry_request_pending: {
      $ref: '#/$defs/RetryRequest',
    },
    create_retry_turn_command: {
      $ref: '#/$defs/CreateRetryTurnCommand',
    },
    create_retry_turn_results: {
      type: 'array',
      minItems: 2,
      items: {
        $ref: '#/$defs/CreateRetryTurnResult',
      },
    },
    retry_turn_created: {
      $ref: '#/$defs/Turn',
    },
    retry_request: {
      $ref: '#/$defs/RetryRequest',
    },
    retry_turn: {
      $ref: '#/$defs/Turn',
    },
    retry_progress_decision: {
      type: 'object',
      additionalProperties: false,
      required: [
        'decided_by',
        'counts_toward_effective_turn_limit',
      ],
      properties: {
        decided_by: {
          const: 'PracticeSessionPolicy',
        },
        counts_toward_effective_turn_limit: {
          type: 'boolean',
        },
      },
    },
    history_records: {
      type: 'array',
      items: {
        $ref: '#/$defs/HistoryRecord',
      },
    },
  },
  $defs: formalDefinitions,
};

const ajv = new Ajv2020({
  allErrors: true,
  strict: true,
  strictRequired: false,
});
addFormats(ajv);
ajv.addKeyword({
  keyword: 'x-internal',
  schemaType: 'boolean',
});
const validateFixture = ajv.compile(fixtureSchema);
if (!validateFixture(fixture)) {
  const errors = validateFixture.errors
    .map(
      ({ instancePath, keyword, message }) =>
        `${instancePath || '/'} ${keyword}: ${message}`,
    )
    .join('\n');
  throw new Error(`The main-flow fixture violates the OpenAPI DTOs:\n${errors}`);
}

const assertFixtureMutationRejected = (caseName, mutate) => {
  const invalidFixture = structuredClone(fixture);
  mutate(invalidFixture);
  assert.equal(
    validateFixture(invalidFixture),
    false,
    `${caseName}: invalid fixture mutation was accepted`,
  );
};

assertFixtureMutationRejected('pending analysis with score', (invalid) => {
  invalid.turn_analyses[1].score = 10;
});
assertFixtureMutationRejected(
  'completed analysis with failure fields',
  (invalid) => {
    invalid.turn_analyses[0].failure_reason = 'must not coexist';
    invalid.turn_analyses[0].failed_at = '2026-07-23T10:05:05Z';
  },
);
assertFixtureMutationRejected(
  'pending retry with a created Turn',
  (invalid) => {
    invalid.retry_request_pending.new_turn_id = 'turn_invalid';
  },
);
assertFixtureMutationRejected(
  'created retry with a failure reason',
  (invalid) => {
    invalid.retry_request.failure_reason = 'must not coexist';
  },
);
assertFixtureMutationRejected(
  'answering Turn with a final answer',
  (invalid) => {
    invalid.retry_turn_created.answer_text = 'not submitted yet';
  },
);
assertFixtureMutationRejected(
  'follow-up Question without a parent',
  (invalid) => {
    delete invalid.questions[2].parent_question_id;
  },
);

const sessionId = fixture.practice_session.practice_session_id;
const policy = fixture.session_policy;

assert.ok(policy.min_effective_turns <= policy.coverage_checkpoint_turn);
assert.ok(policy.coverage_checkpoint_turn <= policy.max_effective_turns);

const participantById = new Map(
  fixture.participants.map((participant) => [
    participant.practice_participant_id,
    participant,
  ]),
);
assert.equal(participantById.size, fixture.participants.length);
assert.equal(
  fixture.participants.filter(
    (participant) => participant.participant_role === 'INTERVIEWER',
  ).length,
  1,
);
assert.equal(
  fixture.participants.filter(
    (participant) => participant.participant_role === 'CANDIDATE',
  ).length,
  1,
);
assert.equal(
  new Set(
    fixture.participants.map(
      (participant) =>
        `${participant.subject_ref.namespace}:${participant.subject_ref.subject_id}`,
    ),
  ).size,
  fixture.participants.length,
);
assert.ok(
  fixture.participants.every(
    (participant) => participant.practice_session_id === sessionId,
  ),
);

const questionById = new Map(
  fixture.questions.map((question) => [question.question_id, question]),
);
assert.equal(questionById.size, fixture.questions.length);
for (const question of fixture.questions) {
  assert.equal(question.practice_session_id, sessionId);
  assert.equal(
    participantById.get(question.speaker_participant_id)?.participant_role,
    'INTERVIEWER',
  );
  assert.ok(question.addressee_participant_ids.length > 0);
  assert.ok(
    question.addressee_participant_ids.every((participantId) =>
      participantById.has(participantId),
    ),
  );
  if (question.question_type === 'FOLLOW_UP') {
    assert.ok(questionById.has(question.parent_question_id));
  }
}

assert.equal(
  fixture.effective_turns.length,
  policy.coverage_checkpoint_turn,
);
assert.ok(fixture.effective_turns.length >= policy.min_effective_turns);
assert.ok(fixture.effective_turns.length <= policy.max_effective_turns);
assert.equal(fixture.practice_session.practice_session_status, 'completed');

const effectiveTurnById = new Map(
  fixture.effective_turns.map((turn) => [turn.turn_id, turn]),
);
assert.equal(effectiveTurnById.size, fixture.effective_turns.length);
for (const turn of fixture.effective_turns) {
  const question = questionById.get(turn.question_id);
  assert.ok(question);
  assert.equal(turn.practice_session_id, sessionId);
  assert.equal(turn.sequence, question.sequence);
  assert.ok(
    question.addressee_participant_ids.includes(
      turn.respondent_participant_id,
    ),
  );
  assert.equal(turn.turn_status, 'completed');
}

const analysisById = new Map(
  fixture.turn_analyses.map((analysis) => [
    analysis.turn_analysis_id,
    analysis,
  ]),
);
assert.equal(fixture.turn_analyses.length, fixture.effective_turns.length);
assert.ok(
  fixture.turn_analyses.every((analysis) =>
    effectiveTurnById.has(analysis.turn_id),
  ),
);

const feedbackById = new Map(
  fixture.feedback_items.map((feedback) => [
    feedback.feedback_item_id,
    feedback,
  ]),
);
for (const feedback of fixture.feedback_items) {
  assert.equal(
    analysisById.get(feedback.turn_analysis_id)?.analysis_status,
    'completed',
  );
  assert.ok(feedback.evidence.length > 0);
}

const pendingRetry = fixture.retry_request_pending;
const originalTurn = effectiveTurnById.get(pendingRetry.original_turn_id);
assert.ok(originalTurn);
assert.ok(feedbackById.has(pendingRetry.feedback_item_id));
assert.equal(pendingRetry.retry_status, 'pending');

const createRetryTurnCommand = fixture.create_retry_turn_command;
assert.equal(
  createRetryTurnCommand.retry_request_id,
  pendingRetry.retry_request_id,
);
assert.equal(
  createRetryTurnCommand.original_turn_id,
  pendingRetry.original_turn_id,
);

const [firstCreateRetryTurnResult, ...replayedCreateRetryTurnResults] =
  fixture.create_retry_turn_results;
for (const replayedResult of replayedCreateRetryTurnResults) {
  assert.deepEqual(replayedResult, firstCreateRetryTurnResult);
}
assert.equal(
  firstCreateRetryTurnResult.retry_request_id,
  createRetryTurnCommand.retry_request_id,
);

const createdRetryTurn = fixture.retry_turn_created;
assert.equal(createdRetryTurn.turn_status, 'answering');
assert.equal(
  createdRetryTurn.turn_id,
  firstCreateRetryTurnResult.new_turn_id,
);
assert.equal(createdRetryTurn.practice_session_id, sessionId);
assert.equal(createdRetryTurn.question_id, originalTurn.question_id);
assert.equal(
  createdRetryTurn.respondent_participant_id,
  originalTurn.respondent_participant_id,
);
assert.equal(createdRetryTurn.sequence, originalTurn.sequence);
assert.equal(createdRetryTurn.answer_text, undefined);

const retry = fixture.retry_request;
assert.equal(retry.retry_request_id, pendingRetry.retry_request_id);
assert.equal(retry.original_turn_id, pendingRetry.original_turn_id);
assert.equal(retry.feedback_item_id, pendingRetry.feedback_item_id);
assert.equal(retry.created_at, pendingRetry.created_at);
assert.equal(retry.retry_status, 'turn_created');
assert.equal(retry.new_turn_id, createdRetryTurn.turn_id);

const completedRetryTurn = fixture.retry_turn;
for (const immutableField of [
  'turn_id',
  'practice_session_id',
  'question_id',
  'respondent_participant_id',
  'sequence',
  'created_at',
]) {
  assert.equal(
    completedRetryTurn[immutableField],
    createdRetryTurn[immutableField],
  );
}
assert.equal(completedRetryTurn.turn_status, 'completed');

const retryProgressDecision = fixture.retry_progress_decision;
assert.equal(retryProgressDecision.decided_by, 'PracticeSessionPolicy');
assert.equal(
  effectiveTurnById.has(completedRetryTurn.turn_id),
  retryProgressDecision.counts_toward_effective_turn_limit,
);

for (const historyRecord of fixture.history_records) {
  assert.equal(historyRecord.practice_session_id, sessionId);
  assert.ok(effectiveTurnById.has(historyRecord.turn_id));
  assert.equal(
    analysisById.get(historyRecord.turn_analysis_id)?.analysis_status,
    'completed',
  );
  if (historyRecord.retry_request_id !== undefined) {
    assert.equal(historyRecord.retry_request_id, retry.retry_request_id);
  }
}

console.log(
  `Validated deterministic ${fixture.fixture_id}: ` +
    `${fixture.effective_turns.length} effective Turns and one same-question retry.`,
);
