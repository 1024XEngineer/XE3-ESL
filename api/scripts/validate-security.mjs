import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);
const apiDirectory = fileURLToPath(new URL('..', import.meta.url));
const httpMethods = new Set([
  'get',
  'put',
  'post',
  'delete',
  'options',
  'head',
  'patch',
  'trace',
]);
const bearerSecurity = [{ BearerSession: [] }];
const publicOperations = new Set([
  'GET /health',
  'POST /v1/auth/register',
  'POST /v1/auth/login',
  'GET /v1/scenes',
  'GET /v1/scenes/{scene_id}',
  'GET /v1/scenes/{scene_id}/roles',
  'GET /v1/ielts-speaking/question-bank',
]);
const normalizeFieldName = (fieldName) =>
  String(fieldName ?? '')
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .replaceAll('-', '_')
    .toLowerCase();
const isTrustedIdentityField = (fieldName) => {
  const normalized = normalizeFieldName(fieldName);
  return (
    /^(owner_|current_|authenticated_)?(user|actor)_id$/.test(normalized) ||
    /^(owner_|current_|auth_)?session_id$/.test(normalized)
  );
};
const isRawTokenField = (fieldName) =>
  /^(?:token(?:_(?:value|secret|hash|digest))?|[a-z0-9_]+_token(?:_(?:value|secret|hash|digest))?)$/.test(
    normalizeFieldName(fieldName),
  );
const isForbiddenRequestField = (fieldName) =>
  isTrustedIdentityField(fieldName) ||
  isRawTokenField(fieldName) ||
  normalizeFieldName(fieldName) === 'token_type' ||
  /(authorization|credential|bearer|cookie)/i.test(fieldName) ||
  /(session.*(digest|hash|secret)|(digest|hash|secret).*session)/i.test(
    fieldName,
  );
const isSensitiveResponseField = (fieldName) =>
  /password/i.test(normalizeFieldName(fieldName)) ||
  /credential/i.test(normalizeFieldName(fieldName)) ||
  /(session.*(digest|hash|secret)|(digest|hash|secret).*session)/i.test(
    normalizeFieldName(fieldName),
  );
const containsCredentialValue = (value) =>
  /\bBearer\s+\S+/i.test(value) ||
  /\bsess_[A-Za-z0-9._~+/-]+={0,}/.test(value);

const bundleOpenApi = async () => {
  const temporaryDirectory = await mkdtemp(
    resolve(tmpdir(), 'speakup-security-bundle-'),
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

const openApi = await bundleOpenApi();
const schemas = openApi.components?.schemas ?? {};
const interviewPreparationRequest =
  schemas.CreateInterviewPreparationRequest?.properties ?? {};
assert.equal(interviewPreparationRequest.resume?.format, 'binary');
assert.equal(
  interviewPreparationRequest.resume?.contentMediaType,
  'application/pdf',
);
assert.equal(interviewPreparationRequest.resume_id, undefined);
assert.equal(interviewPreparationRequest.expected_resume_version, undefined);
assert.equal(interviewPreparationRequest.resume_revision, undefined);
assert.equal(interviewPreparationRequest.current_revision, undefined);
assert.equal(interviewPreparationRequest.resource_version, undefined);
assert.equal(schemas.ResumeSnapshot, undefined);
const responses = openApi.components?.responses ?? {};

const resolveLocalReference = (value) => {
  let current = value;
  const visited = new Set();
  while (
    current !== null &&
    typeof current === 'object' &&
    typeof current.$ref === 'string'
  ) {
    assert.match(
      current.$ref,
      /^#\//,
      `Expected a bundled local reference, received ${current.$ref}.`,
    );
    assert.ok(
      !visited.has(current.$ref),
      `Cyclic local reference detected at ${current.$ref}.`,
    );
    visited.add(current.$ref);
    current = current.$ref
      .slice(2)
      .split('/')
      .map((token) => token.replaceAll('~1', '/').replaceAll('~0', '~'))
      .reduce((resolved, token) => resolved?.[token], openApi);
    assert.notEqual(
      current,
      undefined,
      `Unresolved bundled reference ${[...visited].at(-1)}.`,
    );
  }
  return current;
};

const collectOperations = () => {
  const operations = [];
  for (const [path, pathItem] of Object.entries(openApi.paths ?? {})) {
    for (const [method, operation] of Object.entries(pathItem)) {
      if (httpMethods.has(method)) {
        operations.push({
          key: `${method.toUpperCase()} ${path}`,
          method,
          path,
          operation,
          pathParameters: pathItem.parameters ?? [],
        });
      }
    }
  }
  return operations;
};

const collectSchemaPropertyNames = (
  schemaValue,
  names = new Set(),
  visitedReferences = new Set(),
) => {
  if (Array.isArray(schemaValue)) {
    for (const item of schemaValue) {
      collectSchemaPropertyNames(item, names, visitedReferences);
    }
    return names;
  }
  if (schemaValue === null || typeof schemaValue !== 'object') {
    return names;
  }

  if (typeof schemaValue.$ref === 'string') {
    if (!visitedReferences.has(schemaValue.$ref)) {
      visitedReferences.add(schemaValue.$ref);
      collectSchemaPropertyNames(
        resolveLocalReference(schemaValue),
        names,
        visitedReferences,
      );
    }
  }

  for (const propertyName of Object.keys(schemaValue.properties ?? {})) {
    names.add(propertyName);
  }
  for (const [key, item] of Object.entries(schemaValue)) {
    if (key === '$ref') {
      continue;
    }
    collectSchemaPropertyNames(item, names, visitedReferences);
  }
  return names;
};

const getJsonSchema = (contentOwner) =>
  resolveLocalReference(contentOwner)?.content?.['application/json']?.schema;
const getContentSchemas = (contentOwner) =>
  Object.values(resolveLocalReference(contentOwner)?.content ?? {})
    .map((mediaType) => mediaType?.schema)
    .filter((schema) => schema !== undefined);
const getContentExamples = (contentOwner) => {
  const examples = [];
  const resolvedOwner = resolveLocalReference(contentOwner);
  for (const mediaTypeValue of Object.values(resolvedOwner?.content ?? {})) {
    const mediaType = resolveLocalReference(mediaTypeValue);
    if (mediaType?.example !== undefined) {
      examples.push(mediaType.example);
    }
    for (const exampleValue of Object.values(mediaType?.examples ?? {})) {
      const example = resolveLocalReference(exampleValue);
      examples.push(example?.value ?? example);
    }
  }
  return examples;
};
const collectObjectKeys = (value, keys = new Set()) => {
  if (Array.isArray(value)) {
    for (const item of value) {
      collectObjectKeys(item, keys);
    }
    return keys;
  }
  if (value === null || typeof value !== 'object') {
    return keys;
  }
  for (const [key, item] of Object.entries(value)) {
    keys.add(key);
    collectObjectKeys(item, keys);
  }
  return keys;
};
const collectStrings = (value, strings = []) => {
  if (typeof value === 'string') {
    strings.push(value);
    return strings;
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      collectStrings(item, strings);
    }
    return strings;
  }
  if (value === null || typeof value !== 'object') {
    return strings;
  }
  for (const item of Object.values(value)) {
    collectStrings(item, strings);
  }
  return strings;
};
const collectSchemaDeclaredValues = (
  schemaValue,
  values = [],
  visitedReferences = new Set(),
) => {
  if (Array.isArray(schemaValue)) {
    for (const item of schemaValue) {
      collectSchemaDeclaredValues(item, values, visitedReferences);
    }
    return values;
  }
  if (schemaValue === null || typeof schemaValue !== 'object') {
    return values;
  }

  if (typeof schemaValue.$ref === 'string') {
    if (!visitedReferences.has(schemaValue.$ref)) {
      visitedReferences.add(schemaValue.$ref);
      collectSchemaDeclaredValues(
        resolveLocalReference(schemaValue),
        values,
        visitedReferences,
      );
    }
  }

  for (const key of ['example', 'default', 'const', 'examples', 'enum']) {
    if (schemaValue[key] !== undefined) {
      values.push(schemaValue[key]);
    }
  }
  for (const [key, item] of Object.entries(schemaValue)) {
    if (
      key === '$ref' ||
      ['example', 'default', 'const', 'examples', 'enum'].includes(key)
    ) {
      continue;
    }
    collectSchemaDeclaredValues(item, values, visitedReferences);
  }
  return values;
};
const getDeclaredValues = (value) => {
  const resolved = resolveLocalReference(value);
  const declaredValues = [];
  for (const key of ['example', 'default', 'const']) {
    if (resolved?.[key] !== undefined) {
      declaredValues.push(resolved[key]);
    }
  }
  for (const exampleValue of Object.values(resolved?.examples ?? {})) {
    const example = resolveLocalReference(exampleValue);
    declaredValues.push(example?.value ?? example);
  }
  collectSchemaDeclaredValues(resolved?.schema, declaredValues);
  for (const schema of getContentSchemas(resolved)) {
    collectSchemaDeclaredValues(schema, declaredValues);
  }
  declaredValues.push(...getContentExamples(resolved));
  return declaredValues;
};
const assertNoCredentialValues = (value, context) => {
  for (const stringValue of collectStrings(value)) {
    assert.ok(
      !containsCredentialValue(stringValue),
      `${context} contains a credential-like value.`,
    );
  }
};
const sorted = (values) => [...values].sort();

for (const field of [
  'user_id',
  'actor_id',
  'session_id',
  'session_token',
  'authToken',
  'opaque_token',
  'opaque_session_token',
  'bearer_session_token',
  'authorization_token',
  'token_type',
  'authorization',
  'credential',
  'session_digest',
  'userId',
  'actorId',
  'sessionId',
  'owner_user_id',
]) {
  assert.ok(
    isForbiddenRequestField(field),
    `Sensitive field accepted: ${field}`,
  );
}
for (const field of [
  'email',
  'password',
  'practice_session_id',
  'max_tokens',
  'input_tokens',
  'output_token_count',
  'token_budget',
]) {
  assert.ok(
    !isForbiddenRequestField(field),
    `Legitimate request field rejected: ${field}`,
  );
}
assert.ok(
  collectSchemaPropertyNames({
    $ref: '#/components/schemas/RegisterRequest',
    properties: {
      session_id: {
        type: 'string',
      },
    },
  }).has('session_id'),
  '$ref sibling properties must participate in sensitive-field checks.',
);
assert.ok(containsCredentialValue('Bearer sess_secret'));
assert.ok(containsCredentialValue('debug value sess_secret'));
assert.ok(!containsCredentialValue('Bearer'));
assert.ok(
  collectSchemaDeclaredValues({
    type: 'object',
    properties: {
      reason: {
        type: 'string',
        example: 'Bearer sess_secret',
      },
    },
  }).includes('Bearer sess_secret'),
  'Nested Schema examples must participate in credential-value checks.',
);

const operations = collectOperations();
const operationByKey = new Map(
  operations.map((operation) => [operation.key, operation]),
);
assert.equal(
  operationByKey.size,
  operations.length,
  'Every HTTP method and path pair must be unique.',
);

const bearerScheme = openApi.components?.securitySchemes?.BearerSession;
assert.deepEqual(
  openApi.security,
  bearerSecurity,
  'The root security policy must fail closed with BearerSession.',
);
assert.equal(bearerScheme?.type, 'http');
assert.equal(bearerScheme?.scheme, 'bearer');
assert.equal(bearerScheme?.bearerFormat, 'OpaqueSessionToken');
assert.match(bearerScheme?.description ?? '', /opaque/i);
assert.match(bearerScheme?.description ?? '', /not a JWT/i);
for (const securityScheme of Object.values(
  openApi.components?.securitySchemes ?? {},
)) {
  assert.notEqual(
    securityScheme?.in,
    'cookie',
    'Cookie-based security schemes are outside the v1 contract.',
  );
}

const actualPublicOperations = new Set();
for (const { key, operation } of operations) {
  const effectiveSecurity = operation.security ?? openApi.security;
  if (Array.isArray(effectiveSecurity) && effectiveSecurity.length === 0) {
    actualPublicOperations.add(key);
    continue;
  }

  assert.deepEqual(
    effectiveSecurity,
    bearerSecurity,
    `${key} must inherit or declare BearerSession.`,
  );
  assert.ok(
    operation.responses?.['401'],
    `${key} must document an Unauthorized response.`,
  );
  const unauthorizedReference = operation.responses['401'].$ref;
  assert.ok(
    unauthorizedReference === '#/components/responses/Unauthorized' ||
      unauthorizedReference === '#/components/responses/PrivateUnauthorized' ||
      unauthorizedReference ===
        '#/components/responses/RetryPrivateUnauthorized',
    `${key} must reuse an approved Unauthorized response.`,
  );
  if (
    unauthorizedReference === '#/components/responses/PrivateUnauthorized' ||
    unauthorizedReference ===
      '#/components/responses/RetryPrivateUnauthorized'
  ) {
    const privateUnauthorized = resolveLocalReference(
      operation.responses['401'],
    );
    assert.equal(
      resolveLocalReference(
        privateUnauthorized?.headers?.['Cache-Control'],
      )?.schema?.const,
      'private, no-store',
      `${key} private Unauthorized response must prohibit caching.`,
    );
  }
}
assert.deepEqual(
  sorted(actualPublicOperations),
  sorted(publicOperations),
  'The explicit anonymous operation whitelist changed.',
);

for (const operationKey of publicOperations) {
  assert.ok(
    operationByKey.has(operationKey),
    `Missing public operation ${operationKey}.`,
  );
}

const requireOperation = (key) => {
  const operation = operationByKey.get(key);
  assert.ok(operation, `Missing required operation ${key}.`);
  return operation.operation;
};
const register = requireOperation('POST /v1/auth/register');
const login = requireOperation('POST /v1/auth/login');
const logout = requireOperation('POST /v1/auth/logout');
const me = requireOperation('GET /v1/me');
const getProfile = requireOperation('GET /v1/me/profile');
const updateProfile = requireOperation('PATCH /v1/me/profile');
const uploadAvatar = requireOperation('POST /v1/me/avatar');
const useDefaultAvatar = requireOperation('DELETE /v1/me/avatar');
const getAvatarContent = requireOperation('GET /v1/me/avatar/content');
const createPracticePlan = requireOperation('POST /v1/practice-plans');
const archivePracticePlan = requireOperation(
  'DELETE /v1/practice-plans/{practice_plan_id}',
);

assert.equal(register.operationId, 'registerUser');
assert.equal(login.operationId, 'loginUser');
assert.equal(logout.operationId, 'logoutCurrentSession');
assert.equal(me.operationId, 'getCurrentUser');
assert.equal(getProfile.operationId, 'getCurrentUserProfile');
assert.equal(updateProfile.operationId, 'updateCurrentUserProfile');
assert.equal(uploadAvatar.operationId, 'replaceCurrentUserAvatar');
assert.equal(useDefaultAvatar.operationId, 'useDefaultCurrentUserAvatar');
assert.equal(getAvatarContent.operationId, 'getCurrentUserAvatarContent');
assert.equal(createPracticePlan.operationId, 'createPracticePlan');
assert.equal(archivePracticePlan.operationId, 'archivePracticePlan');
assert.ok(register.requestBody?.required);
assert.ok(login.requestBody?.required);
assert.ok(createPracticePlan.requestBody?.required);
assert.equal(
  getJsonSchema(register.requestBody)?.$ref,
  '#/components/schemas/RegisterRequest',
);
assert.equal(
  getJsonSchema(login.requestBody)?.$ref,
  '#/components/schemas/LoginRequest',
);
assert.equal(
  getJsonSchema(createPracticePlan.requestBody)?.$ref,
  '#/components/schemas/CreatePracticePlanRequest',
);
assert.deepEqual(
  createPracticePlan.security ?? openApi.security,
  bearerSecurity,
  'Preparation Plan creation must resolve its Actor from BearerSession.',
);
assert.deepEqual(
  archivePracticePlan.security ?? openApi.security,
  bearerSecurity,
  'Preparation Plan archival must resolve its Actor from BearerSession.',
);
assert.ok(archivePracticePlan.responses?.['204']);
assert.ok(archivePracticePlan.responses?.['404']);
assert.ok(register.responses?.['201']);
assert.ok(register.responses?.['409']);
assert.ok(register.responses?.['429']);
assert.ok(login.responses?.['200']);
assert.ok(login.responses?.['401']);
assert.ok(login.responses?.['429']);
assert.ok(logout.responses?.['204']);
assert.ok(me.responses?.['200']);
assert.ok(getProfile.responses?.['200']);
assert.ok(getProfile.responses?.['404']);
assert.ok(updateProfile.responses?.['200']);
assert.ok(updateProfile.responses?.['409']);
assert.ok(updateProfile.requestBody?.required);
assert.ok(uploadAvatar.requestBody?.required);
assert.ok(uploadAvatar.responses?.['200']);
assert.ok(uploadAvatar.responses?.['409']);
assert.ok(uploadAvatar.responses?.['413']);
assert.ok(useDefaultAvatar.responses?.['200']);
assert.ok(useDefaultAvatar.responses?.['409']);
assert.ok(getAvatarContent.responses?.['200']);
assert.ok(getAvatarContent.responses?.['404']);
assert.equal(
  getJsonSchema(updateProfile.requestBody)?.$ref,
  '#/components/schemas/UpdateUserProfileRequest',
);
assert.equal(updateProfile.parameters, undefined);
assert.ok(createPracticePlan.responses?.['201']);
assert.equal(logout.requestBody, undefined);
assert.equal(logout.responses?.['429'], undefined);
assert.equal(me.responses?.['429'], undefined);
assert.equal(
  resolveLocalReference(register.responses['201'])?.content?.[
    'application/json'
  ]?.schema?.$ref,
  '#/components/schemas/User',
);
assert.equal(
  register.responses['409']?.$ref,
  '#/components/responses/RegistrationUnavailable',
);
assert.equal(
  register.responses['429']?.$ref,
  '#/components/responses/TooManyRequests',
);
assert.equal(
  resolveLocalReference(login.responses['200'])?.content?.['application/json']
    ?.schema?.$ref,
  '#/components/schemas/LoginResponse',
);
assert.equal(
  login.responses['401']?.$ref,
  '#/components/responses/InvalidCredentials',
);
assert.equal(
  login.responses['429']?.$ref,
  '#/components/responses/TooManyRequests',
);
const loginSuccess = resolveLocalReference(login.responses['200']);
assert.equal(
  loginSuccess?.headers?.['Cache-Control']?.schema?.const,
  'no-store',
);
assert.equal(loginSuccess?.headers?.Pragma?.schema?.const, 'no-cache');
assert.equal(
  resolveLocalReference(me.responses['200'])?.content?.['application/json']
    ?.schema?.$ref,
  '#/components/schemas/User',
);
assert.ok(
  register.requestBody?.content?.['application/json']?.example,
  'Register must provide a request example.',
);
assert.ok(
  login.requestBody?.content?.['application/json']?.example,
  'Login must provide a request example.',
);
const createPracticePlanRequestExample =
  createPracticePlan.requestBody?.content?.['application/json']?.example;
assert.ok(
  createPracticePlanRequestExample,
  'Practice Plan creation must provide a request example.',
);
assert.ok(createPracticePlanRequestExample.source_thread_id);
const createPracticePlanResponse = resolveLocalReference(
  createPracticePlan.responses['201'],
);
assert.equal(
  createPracticePlanResponse?.content?.['application/json']?.schema?.$ref,
  '#/components/schemas/PracticePlan',
);
const createPracticePlanRequestSchema = schemas.CreatePracticePlanRequest;
assert.deepEqual(sorted(createPracticePlanRequestSchema?.required ?? []), [
  'practice_option_id',
  'scene_id',
  'scene_version',
  'selected_role_ids',
]);
assert.deepEqual(
  sorted(Object.keys(createPracticePlanRequestSchema?.properties ?? {})),
  [
    'background_summary',
  'expected_interview_version',
  'ielts_prepared_answers',
  'ielts_selection',
    'interview_preparation_id',
    'max_effective_turns',
    'practice_option_id',
    'scene_id',
    'scene_version',
    'selected_role_ids',
    'source_thread_id',
  ],
);
assert.equal(createPracticePlanRequestSchema?.additionalProperties, false);
assert.equal(createPracticePlanRequestSchema?.oneOf, undefined);
assert.equal(
  createPracticePlanRequestSchema?.properties?.ielts_selection?.$ref,
  '#/components/schemas/IELTSQuestionSelection',
);
assert.equal(
  createPracticePlanRequestSchema?.properties?.max_effective_turns?.maximum,
  64,
  'Practice Plan requests must share the runtime turn safety limit.',
);
const ieltsPracticeSelectionSchema = schemas.IELTSQuestionSelection;
assert.deepEqual(
  sorted(Object.keys(ieltsPracticeSelectionSchema?.properties ?? {})),
  ['cue_card_type', 'part_1_set_id', 'topic_group_id'],
);
assert.equal(
  createPracticePlanRequestSchema?.properties?.source_thread_id?.format,
  'uuid',
);
assert.equal(
  createPracticePlanRequestSchema?.properties?.interview_preparation_id?.$ref,
  '#/components/schemas/AggregateId',
);

const practicePlanSchema = schemas.PracticePlan;
assert.ok(!practicePlanSchema?.required?.includes('source_thread_id'));
assert.ok(practicePlanSchema?.required?.includes('preparation_snapshot'));
assert.ok(practicePlanSchema?.required?.includes('scene_selection'));
assert.ok(practicePlanSchema?.required?.includes('session_policy'));
assert.ok(practicePlanSchema?.required?.includes('practice_objectives'));
assert.ok(!practicePlanSchema?.required?.includes('ielts_assignment'));
assert.equal(
  practicePlanSchema?.properties?.practice_plan_id?.$ref,
  '#/components/schemas/AggregateId',
);
assert.equal(
  practicePlanSchema?.properties?.source_thread_id?.format,
  'uuid',
);
assert.deepEqual(
  practicePlanSchema?.properties?.practice_plan_status?.enum,
  ['draft', 'ready', 'archived'],
);
assert.equal(
  practicePlanSchema?.properties?.ielts_assignment?.$ref,
  '#/components/schemas/IELTSAssignment',
);
const ieltsAssignmentSchema = schemas.IELTSAssignment;
assert.deepEqual(
  sorted(ieltsAssignmentSchema?.required ?? []),
  ['bank_id', 'mode', 'parts', 'season'],
);
assert.equal(ieltsAssignmentSchema?.additionalProperties, false);
assert.equal(
  ieltsAssignmentSchema?.properties?.parts?.items?.$ref,
  '#/components/schemas/IELTSAssignmentPart',
);
const ieltsPartAssignmentSchema = schemas.IELTSAssignmentPart;
assert.deepEqual(
  sorted(ieltsPartAssignmentSchema?.required ?? []),
  ['part', 'source_id', 'turn_blueprints'],
);
assert.deepEqual(ieltsPartAssignmentSchema?.properties?.part?.enum, [
  'PART_1',
  'PART_2',
  'PART_3',
]);
assert.equal(ieltsPartAssignmentSchema?.additionalProperties, false);

const userSchema = schemas.User;
assert.deepEqual(sorted(userSchema?.required ?? []), ['email', 'user_id']);
assert.equal(userSchema?.additionalProperties, false);
assert.equal(userSchema?.properties?.user_id?.readOnly, true);
for (const requestSchemaName of ['RegisterRequest', 'LoginRequest']) {
  const requestSchema = schemas[requestSchemaName];
  assert.deepEqual(sorted(requestSchema?.required ?? []), [
    'email',
    'password',
  ]);
  const expectedProperties =
    requestSchemaName === 'RegisterRequest'
      ? ['display_name', 'email', 'password']
      : ['email', 'password'];
  assert.deepEqual(
    sorted(Object.keys(requestSchema?.properties ?? {})),
    expectedProperties,
  );
  assert.equal(requestSchema?.additionalProperties, false);
}
const userProfileSchema = schemas.UserProfile;
assert.deepEqual(sorted(userProfileSchema?.required ?? []), [
  'created_at',
  'display_name',
  'profile_version',
  'updated_at',
  'user_id',
]);
assert.equal(userProfileSchema?.additionalProperties, false);
assert.equal(
  userProfileSchema?.properties?.avatar?.$ref,
  '#/components/schemas/UserProfileAvatar',
);
const userProfileAvatarSchema = schemas.UserProfileAvatar;
assert.deepEqual(sorted(userProfileAvatarSchema?.required ?? []), [
  'height',
  'updated_at',
  'width',
]);
assert.equal(userProfileAvatarSchema?.additionalProperties, false);
assert.equal(userProfileSchema?.properties?.user_id?.readOnly, true);
assert.equal(
  userProfileSchema?.properties?.profile_version?.readOnly,
  true,
);
const updateProfileRequestSchema = schemas.UpdateUserProfileRequest;
assert.deepEqual(updateProfileRequestSchema?.required, ['display_name']);
assert.deepEqual(
  sorted(Object.keys(updateProfileRequestSchema?.properties ?? {})),
  ['display_name', 'expected_profile_version'],
);
assert.equal(updateProfileRequestSchema?.additionalProperties, false);
const passwordSchema = schemas.Password;
assert.equal(passwordSchema?.writeOnly, true);
assert.equal(passwordSchema?.minLength, 8);
assert.equal(passwordSchema?.maxLength, 128);
const loginPasswordSchema = schemas.LoginPassword;
assert.equal(loginPasswordSchema?.writeOnly, true);
assert.equal(loginPasswordSchema?.minLength, 1);
assert.equal(loginPasswordSchema?.maxLength, 128);
const emailInputPattern = new RegExp(schemas.EmailInput?.pattern, 'u');
for (const email of [
  'learner@example.com',
  'First.Last+practice@xn--fsqu00a.xn--0zwm56d',
  '\t learner@example.com \r\n',
]) {
  assert.match(email, emailInputPattern, `Valid email rejected: ${email}`);
}
for (const email of [
  '.learner@example.com',
  'learner.@example.com',
  'learn..er@example.com',
  'learner@localhost',
  'learn er@example.com',
  '学习者@example.com',
  `${'a'.repeat(250)}@example.com`,
]) {
  assert.doesNotMatch(
    email,
    emailInputPattern,
    `Invalid email accepted: ${email}`,
  );
}
const canonicalEmailPattern = new RegExp(schemas.Email?.pattern, 'u');
assert.match('learner@example.com', canonicalEmailPattern);
assert.doesNotMatch('Learner@example.com', canonicalEmailPattern);
const loginResponseSchema = schemas.LoginResponse;
assert.deepEqual(sorted(loginResponseSchema?.required ?? []), [
  'expires_at',
  'session_token',
  'token_type',
  'user',
]);
assert.equal(
  resolveLocalReference(loginResponseSchema?.properties?.token_type)?.const,
  'Bearer',
);
assert.equal(
  resolveLocalReference(
    loginResponseSchema?.properties?.session_token,
  )?.readOnly,
  true,
);
const opaqueSessionToken = schemas.OpaqueSessionToken;
const opaqueSessionTokenPattern = new RegExp(
  opaqueSessionToken?.pattern,
  'u',
);
for (const token of ['sess_abc123-._~+/', 'sess_abc123==']) {
  assert.match(token, opaqueSessionTokenPattern);
}
for (const token of [
  'abc123==',
  'sess_',
  'sess_contains whitespace',
  'sess_line\nbreak',
  'sess_padding=inside',
]) {
  assert.doesNotMatch(token, opaqueSessionTokenPattern);
}

const errorCode = schemas.ErrorCode;
const expectedIdentityErrors = new Map([
  ['authentication_required', 401],
  ['invalid_credentials', 401],
  ['account_registration_unavailable', 409],
  ['rate_limited', 429],
]);
for (const [errorName, status] of expectedIdentityErrors) {
  assert.ok(errorCode?.enum?.includes(errorName), `Missing ${errorName}.`);
  assert.equal(
    errorCode?.['x-http-status-map']?.[errorName],
    status,
    `${errorName} must map to HTTP ${status}.`,
  );
}
for (const [errorName, status] of new Map([
  ['resource_processing', 409],
  ['provider_unavailable', 503],
  ['quota_exhausted', 503],
])) {
  assert.ok(errorCode?.enum?.includes(errorName), `Missing ${errorName}.`);
  assert.equal(
    errorCode?.['x-http-status-map']?.[errorName],
    status,
    `${errorName} must map to HTTP ${status}.`,
  );
}
assert.equal(
  responses.Unauthorized?.headers?.['WWW-Authenticate']?.schema?.const,
  'Bearer',
);
assert.ok(responses.TooManyRequests?.headers?.['Retry-After']);
assert.ok(responses.Conflict?.headers?.['Retry-After']);
assert.ok(responses.DefaultError?.headers?.['Retry-After']);
assert.equal(
  responses.Unauthorized?.content?.['application/json']?.example?.error?.code,
  'authentication_required',
);
assert.equal(
  responses.TooManyRequests?.content?.['application/json']?.example?.error
    ?.code,
  'rate_limited',
);
assert.equal(
  responses.InvalidCredentials?.content?.['application/json']?.example?.error
    ?.code,
  'invalid_credentials',
);
assert.equal(
  responses.RegistrationUnavailable?.content?.['application/json']?.example
    ?.error?.code,
  'account_registration_unavailable',
);

const tokenResponseLocations = new Set();
for (const { key, operation, pathParameters } of operations) {
  for (const requestSchema of getContentSchemas(operation.requestBody)) {
    const requestFields = collectSchemaPropertyNames(requestSchema);
    for (const field of requestFields) {
      assert.ok(
        !isForbiddenRequestField(field),
        `${key} request must not accept trusted ${field}.`,
      );
    }
    for (const declaredValue of collectSchemaDeclaredValues(requestSchema)) {
      assertNoCredentialValues(declaredValue, `${key} request schema example`);
    }
  }
  for (const example of getContentExamples(operation.requestBody)) {
    assertNoCredentialValues(example, `${key} request example`);
  }

  for (const parameterValue of [
    ...pathParameters,
    ...(operation.parameters ?? []),
  ]) {
    const parameter = resolveLocalReference(parameterValue);
    const parameterName = parameter?.name?.toLowerCase();
    assert.ok(
      !isForbiddenRequestField(parameterName),
      `${key} must not accept trusted parameter ${parameterName}.`,
    );
    const parameterSchemas = [
      parameter?.schema,
      ...getContentSchemas(parameter),
    ].filter((schema) => schema !== undefined);
    for (const parameterSchema of parameterSchemas) {
      const parameterFields = collectSchemaPropertyNames(parameterSchema);
      for (const field of parameterFields) {
        assert.ok(
          !isForbiddenRequestField(field),
          `${key} parameter schema must not accept trusted ${field}.`,
        );
      }
    }
    for (const declaredValue of getDeclaredValues(parameter)) {
      for (const field of collectObjectKeys(declaredValue)) {
        assert.ok(
          !isForbiddenRequestField(field),
          `${key} parameter example must not contain trusted ${field}.`,
        );
      }
      assertNoCredentialValues(declaredValue, `${key} parameter example`);
    }
  }

  for (const [status, responseValue] of Object.entries(
    operation.responses ?? {},
  )) {
    const response = resolveLocalReference(responseValue);
    for (const headerName of Object.keys(response?.headers ?? {})) {
      const header = response.headers[headerName];
      assert.ok(
        !isForbiddenRequestField(headerName.toLowerCase()) &&
          headerName.toLowerCase() !== 'set-cookie',
        `${key} ${status} must not return credentials in ${headerName}.`,
      );
      const headerSchemas = [
        resolveLocalReference(header)?.schema,
        ...getContentSchemas(header),
      ].filter((schema) => schema !== undefined);
      for (const headerSchema of headerSchemas) {
        for (const field of collectSchemaPropertyNames(headerSchema)) {
          assert.ok(
            !isSensitiveResponseField(field) && !isRawTokenField(field),
            `${key} ${status} ${headerName} header exposes ${field}.`,
          );
        }
      }
      for (const declaredValue of getDeclaredValues(header)) {
        for (const field of collectObjectKeys(declaredValue)) {
          assert.ok(
            !isSensitiveResponseField(field) && !isRawTokenField(field),
            `${key} ${status} ${headerName} header example exposes ${field}.`,
          );
        }
        assertNoCredentialValues(
          declaredValue,
          `${key} ${status} ${headerName} header`,
        );
      }
    }
    for (const responseSchema of getContentSchemas(response)) {
      const responseFields = collectSchemaPropertyNames(responseSchema);
      for (const field of responseFields) {
        assert.ok(
          !isSensitiveResponseField(field),
          `${key} ${status} must not expose ${field}.`,
        );
        if (isRawTokenField(field)) {
          tokenResponseLocations.add(`${key} ${status} ${field}`);
        }
      }
      if (!(key === 'POST /v1/auth/login' && status === '200')) {
        for (const declaredValue of collectSchemaDeclaredValues(
          responseSchema,
        )) {
          assertNoCredentialValues(
            declaredValue,
            `${key} ${status} response schema example`,
          );
        }
      }
    }
    for (const example of getContentExamples(response)) {
      for (const field of collectObjectKeys(example)) {
        assert.ok(
          !isSensitiveResponseField(field),
          `${key} ${status} example must not expose ${field}.`,
        );
        if (isRawTokenField(field)) {
          tokenResponseLocations.add(`${key} ${status} ${field}`);
        }
      }
      if (!(key === 'POST /v1/auth/login' && status === '200')) {
        assertNoCredentialValues(example, `${key} ${status} response example`);
      }
    }
  }
}
assert.deepEqual(sorted(tokenResponseLocations), [
  'POST /v1/auth/login 200 session_token',
  'POST /v1/practice-sessions/{practice_session_id}/avatar-session-token 200 session_token',
]);

const agentVoiceTranscription = requireOperation(
  'GET /v1/agent-threads/{thread_id}/voice-transcriptions/realtime',
);
assert.deepEqual(
  agentVoiceTranscription.security ?? openApi.security,
  bearerSecurity,
  'Agent voice transcription must derive the Actor from BearerSession.',
);
const agentVoiceSecurity = agentVoiceTranscription['x-websocket-security'];
assert.equal(agentVoiceSecurity?.credential_location, 'authorization_header');
assert.equal(agentVoiceSecurity?.other_credential_locations_allowed, false);
assert.deepEqual(agentVoiceSecurity?.pre_upgrade?.validation_order, [
  'session',
  'actor',
  'resource_ownership',
  'subprotocol',
  'upgrade',
]);
assert.deepEqual(agentVoiceSecurity?.connection_binding, {
  actor_fields: ['user_id', 'session_id'],
  target_field: 'thread_id',
  target_switch_allowed: false,
});
assert.deepEqual(agentVoiceSecurity?.subprotocol?.allowed, [
  'speakup.voice-input.v1',
]);
const agentVoiceParameters = Object.fromEntries(
  (agentVoiceTranscription.parameters ?? []).map((parameterValue) => {
    const parameter = resolveLocalReference(parameterValue);
    return [parameter.name, parameter];
  }),
);
assert.equal(
  agentVoiceParameters['Sec-WebSocket-Protocol']?.required,
  true,
);
assert.equal(
  resolveLocalReference(
    agentVoiceParameters['Sec-WebSocket-Protocol']?.schema,
  )?.const,
  'speakup.voice-input.v1',
);

for (const [operationKey, operationId] of [
  [
    'GET /v1/practice-sessions/{practice_session_id}/evaluation',
    'getPracticeSessionEvaluation',
  ],
  [
    'GET /v1/practice-turns/{turn_id}/evaluation',
    'getPracticeTurnEvaluation',
  ],
  [
    'GET /v1/agent-messages/{message_id}/evaluation',
    'getAgentMessageEvaluation',
  ],
]) {
  const operation = requireOperation(operationKey);
  assert.equal(operation.operationId, operationId);
  assert.deepEqual(
    operation.security ?? openApi.security,
    bearerSecurity,
    `${operationKey} must derive the Actor from BearerSession.`,
  );
  for (const status of ['200', '401', '404', 'default']) {
    assert.ok(operation.responses?.[status], `${operationKey} must declare ${status}.`);
  }
  assert.equal(
    getJsonSchema(resolveLocalReference(operation.responses['200']))?.$ref,
    '#/components/schemas/EvaluationResource',
  );
}

const retrySessionEvaluation = requireOperation(
  'POST /v1/practice-sessions/{practice_session_id}/evaluation/retry',
);
assert.equal(
  retrySessionEvaluation.operationId,
  'retryPracticeSessionEvaluation',
);
assert.deepEqual(
  retrySessionEvaluation.security ?? openApi.security,
  bearerSecurity,
  'Session Evaluation retry must derive the Actor from BearerSession.',
);
assert.equal(retrySessionEvaluation.requestBody, undefined);
for (const status of ['200', '202', '401', '404', '409', 'default']) {
  assert.ok(
    retrySessionEvaluation.responses?.[status],
    `Session Evaluation retry must declare ${status}.`,
  );
}

const evaluationReportHistory = requireOperation('GET /v1/evaluation-reports');
const evaluationReportHistoryParameters = Object.fromEntries(
  (evaluationReportHistory.parameters ?? []).map((parameterValue) => {
    const parameter = resolveLocalReference(parameterValue);
    return [parameter.name, parameter];
  }),
);
const evaluationReportCursorPattern = resolveLocalReference(
  evaluationReportHistoryParameters.cursor?.schema,
)?.pattern;
const evaluationReportHistoryResponse = resolveLocalReference(
  evaluationReportHistory.responses?.['200'],
);
const evaluationReportHistorySchema = resolveLocalReference(
  getJsonSchema(evaluationReportHistoryResponse),
);
const evaluationReportNextCursorPattern = resolveLocalReference(
  evaluationReportHistorySchema?.properties?.next_cursor,
)?.pattern;
assert.equal(
  evaluationReportCursorPattern,
  evaluationReportNextCursorPattern,
  'Evaluation report request cursor must accept the server next_cursor format.',
);
assert.match(
  'eyJjcmVhdGVkX2F0IjoiMjAyNi0wOC0xNVQwMDowMDowMFoiLCJyZXBvcnRfaWQiOiI3MzExYWRiNC0xZWEwLTQxYzctOGM2ZC1mMzM2Zjg1NGYxYzYifQ',
  new RegExp(evaluationReportCursorPattern, 'u'),
);
assert.doesNotMatch(
  'signed_payload.signed_mac',
  new RegExp(evaluationReportCursorPattern, 'u'),
  'Evaluation cursors are one opaque base64url segment.',
);

const evaluationReport = requireOperation(
  'GET /v1/evaluation-reports/{report_id}',
);
assert.equal(evaluationReport.operationId, 'getEvaluationReport');
assert.deepEqual(
  evaluationReport.security ?? openApi.security,
  bearerSecurity,
  'Evaluation report lookup must derive the Actor from BearerSession.',
);
assert.equal(
  getJsonSchema(resolveLocalReference(evaluationReport.responses['200']))?.$ref,
  '#/components/schemas/StoredFormalReport',
);

for (const schemaName of [
  'EvaluationResource',
  'FormalReport',
  'StoredFormalReport',
  'SpeechEvaluationResult',
  'FeedbackItem',
  'CreateRetryTurnResponse',
]) {
  assert.ok(schemas[schemaName], `The root contract must export ${schemaName}.`);
}

const speechEvaluationResult = resolveLocalReference(
  schemas.SpeechEvaluationResult,
);
const acousticAssessment = resolveLocalReference(
  speechEvaluationResult.properties.acoustic,
);
const assessedAcoustic = acousticAssessment.oneOf
  .map(resolveLocalReference)
  .find((candidate) => candidate.properties?.status?.const === 'ASSESSED');
assert.ok(assessedAcoustic, 'Speech evaluation must declare ASSESSED acoustics.');
assert.equal(assessedAcoustic.properties.provider, undefined);
assert.equal(assessedAcoustic.properties.provider_session, undefined);

const createFeedbackRetryTurn = requireOperation(
  'POST /v1/evaluation-feedback-items/{feedback_item_id}/retry-turns',
);
assert.equal(
  createFeedbackRetryTurn.operationId,
  'createEvaluationFeedbackRetryTurn',
);
assert.equal(
  createFeedbackRetryTurn.requestBody,
  undefined,
  'Retry creation has no client-controlled body.',
);
assert.deepEqual(
  createFeedbackRetryTurn.security ?? openApi.security,
  bearerSecurity,
  'Retry creation must derive the Actor from BearerSession.',
);
for (const status of ['200', '201', '400', '401', '404', '409', 'default']) {
  assert.ok(
    createFeedbackRetryTurn.responses?.[status],
    `Retry creation must declare ${status}.`,
  );
}
const feedbackRetryParameters = (
  createFeedbackRetryTurn.parameters ?? []
).map(resolveLocalReference);
assert.ok(
  feedbackRetryParameters.some(
    (parameter) =>
      parameter.name === 'Idempotency-Key' &&
      parameter.in === 'header' &&
      parameter.required === true,
  ),
  'Retry creation must require Idempotency-Key.',
);
for (const status of ['200', '201']) {
  const response = resolveLocalReference(
    createFeedbackRetryTurn.responses[status],
  );
  assert.equal(
    getJsonSchema(response)?.$ref,
    '#/components/schemas/CreateRetryTurnResponse',
  );
  assert.equal(
    response.headers?.Location,
    undefined,
    'Retry creation must not advertise a nonexistent Turn lookup route.',
  );
}

for (const retiredOperation of [
  'POST /v1/evaluations',
  'GET /v1/evaluations/{evaluation_id}',
  'POST /v1/evaluations/{evaluation_id}/re-evaluate',
  'GET /v1/practice-sessions/{practice_session_id}/interview-report',
  'GET /v1/practice-sessions/{practice_session_id}/ielts-speaking-report',
  'GET /v1/speech-feedback/{speech_feedback_id}',
  'POST /v1/feedback-items/{feedback_item_id}/retry-requests',
  'GET /v1/retry-requests/{retry_request_id}',
  'POST /v1/turns/{turn_id}/turn-analyses',
  'GET /v1/turns/{turn_id}/turn-analyses',
  'GET /v1/turn-analyses/{turn_analysis_id}/feedback-items',
  'GET /v1/history-records',
]) {
  assert.ok(
    !operationByKey.has(retiredOperation),
    `${retiredOperation} is a retired score-bearing smoke contract.`,
  );
}
assert.equal(
  schemas.TurnAnalysis,
  undefined,
  'The score-bearing TurnAnalysis schema must not remain public.',
);
assert.equal(
  schemas.HistoryRecord,
  undefined,
  'The score-bearing HistoryRecord schema must not remain public.',
);
const normalConfirmation = requireOperation(
  'POST /v1/transcription-candidates/{candidate_id}/confirmations',
);
assert.equal(
  normalConfirmation.requestBody,
  undefined,
  'Ordinary confirmation must not accept retry identity in a body.',
);
assert.doesNotMatch(
  normalConfirmation.description ?? '',
  /retry[_ -]?turn/i,
  'Ordinary confirmation must not promise retry behavior.',
);
assert.equal(
  resolveLocalReference(schemas.SubmitTurnRequest)?.properties
    ?.retry_request_id,
  undefined,
  'The legacy Mock submit contract must not accept retry_request_id.',
);

for (const [schemaName, propertyName] of [
  ['AgentMessage', 'speech_feedback_status_url'],
  ['ConfirmedPracticeTurn', 'speech_feedback_status_url'],
]) {
  const schema = resolveLocalReference(schemas[schemaName]);
  assert.ok(
    schema?.properties?.[propertyName],
    `${schemaName} must project ${propertyName}.`,
  );
  assert.ok(
    !(schema.required ?? []).includes(propertyName),
    `${schemaName}.${propertyName} must be absent rather than null when hidden.`,
  );
}
const practiceInteractionState = resolveLocalReference(schemas.PracticeInteractionState);
assert.equal(
  practiceInteractionState?.properties?.ielts_assignment?.$ref,
  '#/components/schemas/IELTSAssignment',
  'PracticeInteractionState must expose the frozen IELTS Part composition.',
);
assert.ok(
  !(practiceInteractionState?.required ?? []).includes('ielts_assignment'),
  'Non-IELTS Practice Sessions must not require an IELTS assignment.',
);
const practiceAssignmentModes = new Map(
  (practiceInteractionState?.allOf ?? [])
    .filter((rule) => rule?.if?.properties?.practice_mode?.const)
    .map((rule) => [
      rule.if.properties.practice_mode.const,
      rule?.then?.properties?.ielts_assignment?.properties?.mode?.const,
    ]),
);
assert.deepEqual(
  practiceAssignmentModes,
  new Map([
    ['FULL_MOCK', 'FULL_MOCK'],
    ['PART_1', 'PART_1'],
    ['PART_2', 'PART_2'],
    ['PART_3', 'PART_3'],
  ]),
  'Practice restore must keep practice_mode and the frozen IELTS mode equal.',
);
const practiceQuestion = resolveLocalReference(schemas.PracticeQuestion);
assert.ok(
  practiceQuestion?.required?.includes('question_type'),
  'PracticeQuestion must expose its PRIMARY/FOLLOW_UP classification.',
);
assert.deepEqual(practiceQuestion.properties.question_type.enum, [
  'PRIMARY',
  'FOLLOW_UP',
]);
const confirmedPracticeTurn = resolveLocalReference(schemas.ConfirmedPracticeTurn);
assert.ok(
  confirmedPracticeTurn?.required?.includes(
    'counts_toward_effective_turn_limit',
  ),
  'ConfirmedPracticeTurn must expose whether it advances the displayed round.',
);
assert.ok(
  practiceInteractionState?.properties?.turn_history,
  'PracticeInteractionState must expose the cold-start Turn history.',
);
assert.equal(
  practiceInteractionState.properties.turn_history.maxItems,
  undefined,
  'User-controlled Turn history must not impose a fixed round-count bound.',
);
assert.equal(
  practiceInteractionState.properties.turn_limit.maximum,
  64,
  'Practice restore must enforce the primary Question safety limit.',
);
assert.ok(
  !(practiceInteractionState.required ?? []).includes('turn_history'),
  'turn_history must remain an optional projection.',
);
assert.match(
  practiceInteractionState.properties.turn_history.description ?? '',
  /EFFECTIVE/,
);
const voiceTurnHistoryEntry = resolveLocalReference(
  practiceInteractionState.properties.turn_history.items,
);
assert.equal(
  voiceTurnHistoryEntry?.properties?.turn?.$ref,
  '#/components/schemas/ConfirmedPracticeTurn',
);
const ordinaryConfirmedTurn = resolveLocalReference(
  voiceTurnHistoryEntry.properties.turn,
);
for (const retryOnlyProperty of [
  'retry_request_id',
  'retry_turn_id',
  'original_turn_id',
  'counts_toward_turn_limit',
]) {
  assert.equal(
    ordinaryConfirmedTurn?.properties?.[retryOnlyProperty],
    undefined,
    `Ordinary ConfirmedPracticeTurn must not expose ${retryOnlyProperty}.`,
  );
}

console.log(
  `Validated ${operations.length} operations: ` +
    `${actualPublicOperations.size} public and ` +
    `${operations.length - actualPublicOperations.size} Bearer-protected.`,
);
