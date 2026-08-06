import { readdir, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';

const apiDirectory = fileURLToPath(new URL('..', import.meta.url));
const envelopePath = resolve(
  apiDirectory,
  'common/event-envelope.schema.json',
);
const eventSchemaPath = resolve(
  apiDirectory,
  'websocket/conversation-events.schema.json',
);
const examplesDirectory = resolve(apiDirectory, 'examples/websocket');

const readJson = async (filePath) =>
  JSON.parse(await readFile(filePath, { encoding: 'utf8' }));

const envelopeSchema = await readJson(envelopePath);
const eventSchema = await readJson(eventSchemaPath);

const ajv = new Ajv2020({ allErrors: true, strict: true });
addFormats(ajv);
ajv.addSchema(envelopeSchema);
const validate = ajv.compile(eventSchema);

const exampleNames = (await readdir(examplesDirectory))
  .filter((name) => name.endsWith('.json'))
  .sort();

if (exampleNames.length === 0) {
  throw new Error('No WebSocket event examples were found.');
}

let hasFailure = false;
const exampleEventTypes = new Set();
for (const exampleName of exampleNames) {
  const example = await readJson(resolve(examplesDirectory, exampleName));
  exampleEventTypes.add(example.event_type);
  if (!validate(example)) {
    hasFailure = true;
    console.error(`${exampleName}: ${ajv.errorsText(validate.errors)}`);
  }
}

const schemaEventTypes = new Set(
  eventSchema.oneOf.map(({ $ref }) => {
    const definitionName = $ref.split('/').at(-1);
    return eventSchema.$defs[definitionName].allOf[1].properties.event_type.const;
  }),
);
const missingExampleTypes = [...schemaEventTypes].filter(
  (eventType) => !exampleEventTypes.has(eventType),
);
if (missingExampleTypes.length > 0) {
  hasFailure = true;
  console.error(
    `Missing positive examples for: ${missingExampleTypes.join(', ')}`,
  );
}

const replayableWithoutSequence = await readJson(
  resolve(examplesDirectory, '02-question-created.json'),
);
delete replayableWithoutSequence.sequence;

const ephemeralWithSequence = await readJson(
  resolve(examplesDirectory, '01-stream-ready.json'),
);
ephemeralWithSequence.sequence = 1;

const payloadWithClientControlledRespondent = await readJson(
  resolve(examplesDirectory, '02-question-created.json'),
);
payloadWithClientControlledRespondent.payload.respondent_participant_id =
  'participant_candidate_001';

const mismatchedTurnStatus = await readJson(
  resolve(examplesDirectory, '03-turn-submitted.json'),
);
mismatchedTurnStatus.payload.turn_status = 'processing';

const unsupportedEventVersion = await readJson(
  resolve(examplesDirectory, '02-question-created.json'),
);
unsupportedEventVersion.event_version = 2;

const followUpWithoutParent = await readJson(
  resolve(examplesDirectory, '02-question-created.json'),
);
followUpWithoutParent.payload.question_type = 'FOLLOW_UP';

const primaryWithParent = await readJson(
  resolve(examplesDirectory, '02-question-created.json'),
);
primaryWithParent.payload.parent_question_id = 'question_parent_001';

const rejectionCases = [
  ['replayable event without sequence', replayableWithoutSequence],
  ['ephemeral event with sequence', ephemeralWithSequence],
  [
    'payload with an undeclared participant field',
    payloadWithClientControlledRespondent,
  ],
  ['event type and Turn status mismatch', mismatchedTurnStatus],
  ['unsupported event version', unsupportedEventVersion],
  ['follow-up Question without parent', followUpWithoutParent],
  ['primary Question with parent', primaryWithParent],
];

for (const [caseName, example] of rejectionCases) {
  if (validate(example)) {
    hasFailure = true;
    console.error(`${caseName}: invalid event was accepted`);
  }
}

if (hasFailure) {
  process.exitCode = 1;
} else {
  console.log(
    `Validated ${exampleNames.length} WebSocket events and ` +
      `${rejectionCases.length} rejection cases.`,
  );
}
