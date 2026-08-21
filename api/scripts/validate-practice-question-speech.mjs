import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import Ajv2020 from 'ajv/dist/2020.js';

const apiDirectory = fileURLToPath(new URL('..', import.meta.url));
const schema = JSON.parse(
  await readFile(
    resolve(apiDirectory, 'websocket/practice-question-speech.schema.json'),
    { encoding: 'utf8' },
  ),
);
const ajv = new Ajv2020({ allErrors: true, strict: true });
ajv.addSchema(schema);
const validate = ajv.compile({
  $ref: `${schema.$id}#/$defs/ServerEvent`,
});
const validateClient = ajv.compile({
  $ref: `${schema.$id}#/$defs/ClientEvent`,
});

const validEvents = [
  {
    type: 'stream.ready',
    data: {
      content_type: 'audio/pcm',
      sample_rate: 24000,
      channel_count: 1,
      bits_per_sample: 16,
    },
  },
  { type: 'stream.completed', data: {} },
  {
    type: 'stream.failed',
    data: { kind: 'synthesis_failed', retryable: true },
  },
];
const invalidEvents = [
  { type: 'stream.ready', data: { content_type: 'audio/wav' } },
  { type: 'stream.completed', data: { audio_url: 'forbidden' } },
  {
    type: 'stream.failed',
    data: {
      kind: 'synthesis_failed',
      retryable: true,
      provider_payload: 'forbidden',
    },
  },
];
for (const event of validEvents) {
  assert.equal(validate(event), true, ajv.errorsText(validate.errors));
}
for (const event of invalidEvents) {
  assert.equal(validate(event), false, `accepted ${event.type}`);
}
assert.equal(validateClient({ type: 'speak', text: 'Try this answer.' }), true);
assert.equal(validateClient({ type: 'speak', text: '' }), false);
assert.equal(
  validateClient({ type: 'speak', text: 'valid', provider: 'forbidden' }),
  false,
);

console.log(
  `Validated ${validEvents.length} Practice Question speech events and ` +
    `${invalidEvents.length} rejections.`,
);
