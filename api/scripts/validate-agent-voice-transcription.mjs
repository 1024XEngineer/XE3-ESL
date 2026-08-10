import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import Ajv2020 from 'ajv/dist/2020.js';

const apiDirectory = fileURLToPath(new URL('..', import.meta.url));
const schema = JSON.parse(
  await readFile(
    resolve(
      apiDirectory,
      'websocket/agent-voice-transcription.schema.json',
    ),
    { encoding: 'utf8' },
  ),
);
const ajv = new Ajv2020({ allErrors: true, strict: true });
ajv.addSchema(schema);
const compileDefinition = (name) =>
  ajv.compile({ $ref: `${schema.$id}#/$defs/${name}` });
const validateClient = compileDefinition('ClientTextFrame');
const validateServer = compileDefinition('ServerEvent');

const validClientFrames = [
  { type: 'start', idempotency_key: 'voice-input-1', sample_rate: 16000 },
  { type: 'start', idempotency_key: '实时转写幂等键值', sample_rate: 16000 },
  { type: 'finish' },
  { type: 'cancel' },
];
const validServerEvents = [
  { type: 'transcription.started', data: {} },
  {
    type: 'transcription.updated',
    data: { transcript: 'I would', final: false },
  },
  {
    type: 'transcription.completed',
    data: { transcript: 'I would like to practice.', final: true },
  },
  {
    type: 'transcription.failed',
    data: { kind: 'timeout', retryable: true },
  },
];
for (const frame of validClientFrames) {
  assert.equal(validateClient(frame), true, ajv.errorsText(validateClient.errors));
}
for (const event of validServerEvents) {
  assert.equal(validateServer(event), true, ajv.errorsText(validateServer.errors));
}

const invalidClientFrames = [
  { type: 'start', idempotency_key: 'short', sample_rate: 16000 },
  { type: 'start', idempotency_key: '🙂🙂', sample_rate: 16000 },
  { type: 'start', idempotency_key: ' voice-input-1 ', sample_rate: 16000 },
  { type: 'start', idempotency_key: 'voice-input-1', sample_rate: 8000 },
  { type: 'finish', transcript: 'client-controlled' },
  { type: 'unknown' },
];
const invalidServerEvents = [
  {
    type: 'transcription.updated',
    data: { transcript: 'Not final.', final: true },
  },
  {
    type: 'transcription.completed',
    data: { transcript: 'Still partial.', final: false },
  },
  { type: 'candidate.ready', data: {} },
  {
    type: 'transcription.failed',
    data: { kind: 'timeout', retryable: true, provider_payload: 'forbidden' },
  },
];
for (const frame of invalidClientFrames) {
  assert.equal(validateClient(frame), false, `accepted client frame ${frame.type}`);
}
for (const event of invalidServerEvents) {
  assert.equal(validateServer(event), false, `accepted server event ${event.type}`);
}

console.log(
  `Validated ${validClientFrames.length} Agent transcription controls, ` +
    `${validServerEvents.length} server events, and ` +
    `${invalidClientFrames.length + invalidServerEvents.length} rejections.`,
);
