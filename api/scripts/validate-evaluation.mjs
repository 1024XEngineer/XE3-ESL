import assert from 'node:assert/strict';
import {execFile} from 'node:child_process';
import {mkdtemp, readFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';
import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';

const execFileAsync = promisify(execFile);
const apiDirectory = fileURLToPath(new URL('..', import.meta.url));
const temporaryDirectory = await mkdtemp(resolve(tmpdir(), 'speakup-evaluation-'));
const bundlePath = resolve(temporaryDirectory, 'openapi.bundle.json');

try {
  await execFileAsync(
    process.execPath,
    [
      resolve(apiDirectory, 'node_modules/@redocly/cli/bin/cli.js'),
      'bundle',
      resolve(apiDirectory, 'openapi.yaml'),
      '--config',
      resolve(apiDirectory, 'redocly.yaml'),
      '--output',
      bundlePath,
      '--ext',
      'json',
    ],
    {cwd: apiDirectory, maxBuffer: 10 * 1024 * 1024},
  );
  const contract = JSON.parse(await readFile(bundlePath, 'utf8'));
  const ajv = new Ajv2020({allErrors: true, strict: false});
  addFormats(ajv);
  ajv.addSchema(contract, 'contract');
  const validate = (schema, value) => {
    const validator = ajv.compile({$ref: `contract#/components/schemas/${schema}`});
    assert.equal(
      validator(value),
      true,
      `${schema}: ${ajv.errorsText(validator.errors, {separator: '\n'})}`,
    );
  };
  const reject = (schema, value) => {
    const validator = ajv.compile({$ref: `contract#/components/schemas/${schema}`});
    assert.equal(validator(value), false, `${schema} unexpectedly accepted value`);
  };

  const evaluationId = '10000000-0000-4000-8000-000000000001';
  const sessionId = '20000000-0000-4000-8000-000000000001';
  const turnId = '30000000-0000-4000-8000-000000000001';
  const createdAt = '2026-08-15T08:00:00Z';
  validate('EvaluationResource', {
    evaluation_id: evaluationId,
    kind: 'SESSION_REPORT',
    source_id: sessionId,
    context_id: sessionId,
    status: 'QUEUED',
    created_at: createdAt,
    updated_at: createdAt,
    feedback_items: [],
  });

  validate('CreateRetryTurnResponse', {
    turn: {
      turn_id: '50000000-0000-4000-8000-000000000001',
      practice_session_id: sessionId,
      question_id: '40000000-0000-4000-8000-000000000001',
      original_turn_id: turnId,
      sequence: 2,
      status: 'confirmed',
      created_at: createdAt,
    },
    replayed: true,
  });

  const pronunciationUnavailable = {
    schema_version: 'evaluation-report/v2',
    scene_type: 'IELTS_SPEAKING',
    practice_experience: 'IELTS_SPEAKING',
    scene_category: 'IELTS_SPEAKING',
    practice_mode: 'FULL_MOCK',
    scoreability_status: 'PROVISIONAL',
    summary: 'The transcript dimensions are ready; pronunciation was unavailable.',
    questions: [
      {
        question_id: '40000000-0000-4000-8000-000000000001',
        position: 1,
        text: 'Tell me about a memorable trip.',
        answer: {
          turn_id: turnId,
          transcript: 'I travelled with my family last summer.',
        },
      },
    ],
    dimensions: [
      {
        key: 'FLUENCY_COHERENCE',
        score: 6.5,
        scale: 'IELTS_BAND_9',
        coverage: 1,
        confidence: 0.8,
        reason_codes: [],
        evidence_ref_ids: [],
        strengths: [],
        improvements: [],
        recommended_examples: [],
      },
      {
        key: 'PRONUNCIATION',
        score: null,
        scale: 'IELTS_BAND_9',
        coverage: 0,
        confidence: 0,
        reason_codes: ['ACOUSTIC_ASSESSMENT_FAILED'],
        evidence_ref_ids: [],
        strengths: [],
        improvements: [],
        recommended_examples: [],
      },
    ],
    priority_actions: [],
  };
  validate('EvaluationResource', {
    evaluation_id: evaluationId,
    kind: 'SESSION_REPORT',
    source_id: sessionId,
    context_id: sessionId,
    status: 'READY',
    created_at: createdAt,
    updated_at: createdAt,
    feedback_items: [],
    result: pronunciationUnavailable,
  });
  validate('StoredFormalReport', {
    report_id: evaluationId,
    evaluation_id: evaluationId,
    practice_session_id: sessionId,
    report: pronunciationUnavailable,
    created_at: createdAt,
  });

  validate('EvaluationResource', {
    evaluation_id: '10000000-0000-4000-8000-000000000002',
    kind: 'PRACTICE_TURN_FEEDBACK',
    source_id: turnId,
    context_id: sessionId,
    status: 'READY',
    created_at: createdAt,
    updated_at: createdAt,
    feedback_items: [],
    result: {
      schema_version: 'speech-feedback/v1',
      scoreability_status: 'PROVISIONAL',
      summary: 'Feedback is ready.',
      reason_codes: [],
      acoustic: {
        status: 'ASSESSED',
        pronunciation: 82,
        fluency: 79,
      },
    },
  });

  const feedbackEvidence = {
    evidence_ref_id: turnId,
    start_utf8_byte: 0,
    end_utf8_byte: 3,
    original_excerpt: 'and',
  };
  reject('FeedbackItem', {
    feedback_item_id: '40000000-0000-4000-8000-000000000001',
    evaluation_id: evaluationId,
    position: 1,
    category: 'STYLE_CORRECTION',
    evidence: feedbackEvidence,
    recommendation: 'Optional wording.',
    correction: 'so',
    repractice_mode: 'SAME_QUESTION',
    created_at: createdAt,
  });
  reject('EvaluationResource', {
    evaluation_id: '10000000-0000-4000-8000-000000000003',
    kind: 'PRACTICE_TURN_FEEDBACK',
    source_id: turnId,
    context_id: sessionId,
    status: 'READY',
    created_at: createdAt,
    updated_at: createdAt,
    feedback_items: [
      {
        feedback_item_id: '40000000-0000-4000-8000-000000000001',
        evaluation_id: '10000000-0000-4000-8000-000000000003',
        position: 1,
        category: 'STRENGTH',
        evidence: feedbackEvidence,
        recommendation: 'No change is needed.',
        repractice_mode: 'NONE',
        created_at: createdAt,
      },
      {
        feedback_item_id: '40000000-0000-4000-8000-000000000002',
        evaluation_id: '10000000-0000-4000-8000-000000000003',
        position: 2,
        category: 'RECOMMENDED_EXPRESSION',
        evidence: feedbackEvidence,
        recommendation: 'Optional wording.',
        correction: 'so',
        repractice_mode: 'SAME_QUESTION',
        created_at: createdAt,
      },
    ],
    result: {
      schema_version: 'speech-feedback/v1',
      scoreability_status: 'PROVISIONAL',
      summary: 'Feedback is ready.',
      reason_codes: [],
      acoustic: {
        status: 'NOT_ASSESSED',
        reason: 'ACOUSTIC_ASSESSMENT_NOT_CONFIGURED',
      },
    },
  });

  const operations = new Set();
  for (const [path, item] of Object.entries(contract.paths)) {
    for (const method of ['get', 'post', 'put', 'patch', 'delete']) {
      if (item[method]) operations.add(`${method.toUpperCase()} ${path}`);
    }
  }
  for (const expected of [
    'GET /v1/practice-sessions/{practice_session_id}/evaluation',
    'GET /v1/practice-turns/{turn_id}/evaluation',
    'GET /v1/agent-messages/{message_id}/evaluation',
    'GET /v1/evaluation-reports',
    'GET /v1/evaluation-reports/{report_id}',
    'POST /v1/evaluation-feedback-items/{feedback_item_id}/retry-turns',
  ]) {
    assert.ok(operations.has(expected), `missing ${expected}`);
  }
  for (const retired of [
    'POST /v1/evaluations',
    'GET /v1/evaluations/{evaluation_id}',
    'POST /v1/evaluations/{evaluation_id}/re-evaluate',
    'GET /v1/speech-feedback/{speech_feedback_id}',
    'POST /v1/feedback-items/{feedback_item_id}/retry-requests',
    'GET /v1/retry-requests/{retry_request_id}',
  ]) {
    assert.ok(!operations.has(retired), `retired operation remains: ${retired}`);
  }

  const assessed = contract.components.schemas.AcousticAssessed;
  assert.equal(assessed.properties.provider, undefined);
  assert.equal(assessed.properties.provider_session, undefined);
  assert.ok(contract.components.schemas.ReportDimension.required.includes('score'));
  const retry = contract.paths[
    '/v1/evaluation-feedback-items/{feedback_item_id}/retry-turns'
  ].post;
  assert.equal(retry.requestBody, undefined);
  assert.equal(retry.responses['201'].headers, undefined);
  assert.deepEqual(
    contract.components.schemas.FeedbackItem.properties.repractice_mode.enum,
    ['NONE', 'SAME_QUESTION'],
  );
  assert.deepEqual(contract.components.schemas.FeedbackItemCategory.enum, [
    'CORRECTION',
    'STRENGTH',
    'RECOMMENDED_EXPRESSION',
  ]);
  assert.deepEqual(
    contract.components.schemas.RetryTurnResource.properties.status.enum,
    ['answering', 'transcribing', 'transcribed', 'confirmed', 'failed'],
  );
  const agentStatusPattern = new RegExp(
    contract.components.schemas.AgentMessage.properties.speech_feedback_status_url.pattern,
  );
  const practiceStatusPattern = new RegExp(
    contract.components.schemas.ConfirmedPracticeTurn.properties.speech_feedback_status_url.pattern,
  );
  assert.match(`/v1/agent-messages/${turnId}/evaluation`, agentStatusPattern);
  assert.match(`/v1/practice-turns/${turnId}/evaluation`, practiceStatusPattern);
  assert.doesNotMatch(`/v1/speech-feedback/${turnId}`, agentStatusPattern);
  assert.doesNotMatch(`/v1/speech-feedback/${turnId}`, practiceStatusPattern);

  console.log('Validated the unified Evaluation lifecycle, report, and retry contract.');
} finally {
  await rm(temporaryDirectory, {recursive: true, force: true});
}
