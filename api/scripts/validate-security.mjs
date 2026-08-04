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
  'GET /v1/scenes/ielts-speaking/question-bank',
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
const preparationProfileRequest =
  schemas.CreatePreparationProfileRequest?.properties ?? {};
const preparationTextPattern =
  preparationProfileRequest.background_summary?.pattern;
assert.equal(preparationProfileRequest.resume_ref?.maxLength, 16 * 1024);
assert.equal(
  preparationProfileRequest.job_description_ref?.maxLength,
  16 * 1024,
);
assert.equal(
  preparationProfileRequest.background_summary?.maxLength,
  64 * 1024,
);
assert.equal(
  preparationProfileRequest.resume_ref?.pattern,
  preparationTextPattern,
);
assert.equal(
  preparationProfileRequest.job_description_ref?.pattern,
  preparationTextPattern,
);
const preparationTextExpression = new RegExp(preparationTextPattern, 'u');
assert.match('a', preparationTextExpression);
assert.match('internal whitespace is allowed', preparationTextExpression);
assert.doesNotMatch(' leading', preparationTextExpression);
assert.doesNotMatch('trailing ', preparationTextExpression);
assert.doesNotMatch('contains\u0000nul', preparationTextExpression);
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
const createPracticePlan = requireOperation('POST /v1/practice-plans');

assert.equal(register.operationId, 'registerUser');
assert.equal(login.operationId, 'loginUser');
assert.equal(logout.operationId, 'logoutCurrentSession');
assert.equal(me.operationId, 'getCurrentUser');
assert.equal(getProfile.operationId, 'getCurrentUserProfile');
assert.equal(updateProfile.operationId, 'updateCurrentUserProfile');
assert.equal(createPracticePlan.operationId, 'createPracticePlan');
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
assert.equal(
  getJsonSchema(updateProfile.requestBody)?.$ref,
  '#/components/schemas/UpdateUserProfileRequest',
);
assert.ok(
  updateProfile.parameters?.some(
    (parameter) =>
      parameter?.$ref === '#/components/parameters/IdempotencyKey',
  ),
  'Profile updates must require the shared Idempotency-Key parameter.',
);
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
assert.ok(createPracticePlanRequestExample.goal_id);
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
  'preparation_snapshot_id',
  'scene_id',
  'scene_version',
  'selected_role_ids',
]);
assert.deepEqual(
  sorted(Object.keys(createPracticePlanRequestSchema?.properties ?? {})),
  [
    'goal_id',
    'ielts_selection',
    'max_effective_turns',
    'practice_option_id',
    'preparation_snapshot_id',
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
  '#/components/schemas/IELTSPracticeSelection',
);
for (const contextField of ['source_thread_id', 'goal_id']) {
  assert.equal(
    createPracticePlanRequestSchema?.properties?.[contextField]?.$ref,
    '#/components/schemas/ResourceId',
    `${contextField} must reuse ResourceId.`,
  );
}

const practicePlanSchema = schemas.PracticePlan;
assert.ok(!practicePlanSchema?.required?.includes('source_thread_id'));
assert.ok(!practicePlanSchema?.required?.includes('goal_snapshot'));
assert.ok(practicePlanSchema?.required?.includes('preparation_snapshot'));
assert.ok(practicePlanSchema?.required?.includes('scene_selection'));
assert.ok(practicePlanSchema?.required?.includes('session_policy'));
assert.ok(practicePlanSchema?.required?.includes('practice_objectives'));
assert.ok(!practicePlanSchema?.required?.includes('ielts_assignment'));
assert.equal(
  practicePlanSchema?.properties?.source_thread_id?.$ref,
  '#/components/schemas/ResourceId',
);
assert.deepEqual(
  practicePlanSchema?.properties?.practice_plan_status?.enum,
  ['ready', 'archived'],
);
assert.equal(
  practicePlanSchema?.properties?.ielts_assignment?.$ref,
  '#/components/schemas/IELTSPracticeAssignment',
);

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

const websocket = requireOperation(
  'GET /v1/practice-sessions/{practice_session_id}/events',
);
const websocketSecurity = websocket['x-websocket-security'];
assert.equal(websocketSecurity?.credential_location, 'authorization_header');
assert.equal(websocketSecurity?.header, 'Authorization');
assert.equal(websocketSecurity?.scheme, 'Bearer');
assert.equal(websocketSecurity?.other_credential_locations_allowed, false);
assert.equal(websocketSecurity?.production_transport, 'wss');
assert.equal(websocketSecurity?.local_loopback_transport, 'ws');
assert.deepEqual(websocketSecurity?.pre_upgrade?.validation_order, [
  'session',
  'actor',
  'resource_ownership',
  'replay_cursor',
  'subprotocol',
  'upgrade',
]);
assert.deepEqual(websocketSecurity?.pre_upgrade?.authentication_failure, {
  http_status: 401,
  error_code: 'authentication_required',
});
assert.deepEqual(websocketSecurity?.pre_upgrade?.resource_not_visible, {
  http_status: 404,
  error_code: 'resource_not_found',
});
assert.equal(websocketSecurity?.reconnect?.reauthenticate, true);
assert.deepEqual(websocketSecurity?.connection_binding, {
  actor_fields: ['user_id', 'session_id'],
  target_field: 'practice_session_id',
  target_switch_allowed: false,
});
assert.deepEqual(websocketSecurity?.logout, {
  close_connections_by: 'session_id',
  all_matching_connections: true,
});
assert.deepEqual(websocketSecurity?.active_connection?.authorization_recheck, {
  before_replay_batch: true,
  before_private_outbound_event: true,
  checks: ['session_validity', 'resource_ownership'],
});
assert.deepEqual(websocketSecurity?.active_connection?.invalid_session, {
  close_code: 4401,
  close_reason: 'session_invalid',
  send_application_events_before_close: false,
});
assert.equal(
  websocketSecurity?.active_connection?.ordinary_disconnect?.is_logout,
  false,
);
assert.deepEqual(websocketSecurity?.subprotocol?.allowed, [
  'speakup.events.v1',
]);
assert.equal(websocketSecurity?.subprotocol?.carries_credentials, false);
assert.equal(websocketSecurity?.after_sequence?.is_credential, false);

const websocketParameters = Object.fromEntries(
  (websocket.parameters ?? []).map((parameterValue) => {
    const parameter = resolveLocalReference(parameterValue);
    return [parameter.name, parameter];
  }),
);
assert.equal(websocketParameters['Sec-WebSocket-Protocol']?.in, 'header');
assert.equal(websocketParameters['Sec-WebSocket-Protocol']?.required, true);
assert.equal(
  resolveLocalReference(
    websocketParameters['Sec-WebSocket-Protocol']?.schema,
  )?.const,
  'speakup.events.v1',
);
assert.equal(
  resolveLocalReference(websocket.responses?.['101'])?.headers?.[
    'Sec-WebSocket-Protocol'
  ]?.schema?.const,
  'speakup.events.v1',
);
assert.equal(websocketParameters.after_sequence?.in, 'query');

const interviewReport = requireOperation(
  'GET /v1/practice-sessions/{practice_session_id}/interview-report',
);
assert.equal(interviewReport.operationId, 'getInterviewReport');
assert.deepEqual(
  interviewReport.security ?? openApi.security,
  bearerSecurity,
  'Interview reports must derive the Actor from BearerSession.',
);
assert.ok(interviewReport.responses?.['200']);
assert.ok(interviewReport.responses?.['401']);
assert.ok(interviewReport.responses?.['404']);
assert.ok(interviewReport.responses?.['409']);
const interviewReportResponse = resolveLocalReference(
  interviewReport.responses['200'],
);
assert.equal(
  interviewReportResponse?.headers?.['Cache-Control']?.schema?.const,
  'private, no-store',
  'Interview reports must prohibit shared and private caching.',
);
assert.equal(
  getJsonSchema(interviewReportResponse)?.$ref,
  '#/components/schemas/InterviewReportEnvelope',
);
assert.ok(
  schemas.InterviewReportEnvelope,
  'The root contract must export InterviewReportEnvelope.',
);
assert.ok(
  schemas.InterviewReport,
  'The root contract must export InterviewReport.',
);
assert.match(interviewReport.description ?? '', /another Actor/i);
assert.match(interviewReport.description ?? '', /must not log/i);

const ieltsSpeakingReport = requireOperation(
  'GET /v1/practice-sessions/{practice_session_id}/ielts-speaking-report',
);
assert.equal(
  ieltsSpeakingReport.operationId,
  'getIeltsSpeakingReport',
);
assert.deepEqual(
  ieltsSpeakingReport.security ?? openApi.security,
  bearerSecurity,
  'IELTS Speaking reports must derive the Actor from BearerSession.',
);
for (const status of ['200', '401', '404', '409']) {
  assert.ok(ieltsSpeakingReport.responses?.[status]);
}
const ieltsSpeakingReportResponse = resolveLocalReference(
  ieltsSpeakingReport.responses['200'],
);
assert.equal(
  ieltsSpeakingReportResponse?.headers?.['Cache-Control']?.schema?.const,
  'private, no-store',
  'IELTS Speaking reports must prohibit shared and private caching.',
);
assert.equal(
  getJsonSchema(ieltsSpeakingReportResponse)?.$ref,
  '#/components/schemas/IeltsSpeakingReportEnvelope',
);
assert.ok(
  schemas.IeltsSpeakingReportEnvelope,
  'The root contract must export IeltsSpeakingReportEnvelope.',
);
assert.ok(
  schemas.IeltsSpeakingReport,
  'The root contract must export IeltsSpeakingReport.',
);
assert.match(ieltsSpeakingReport.description ?? '', /another Actor/i);
assert.match(ieltsSpeakingReport.description ?? '', /must not log/i);

const ieltsSpeakingReportIndex = requireOperation(
  'GET /v1/ielts-speaking-reports',
);
assert.equal(
  ieltsSpeakingReportIndex.operationId,
  'listIeltsSpeakingReports',
);
assert.deepEqual(
  ieltsSpeakingReportIndex.security ?? openApi.security,
  bearerSecurity,
  'IELTS Speaking report history must derive the Actor from BearerSession.',
);
for (const status of ['200', '400', '401']) {
  assert.ok(ieltsSpeakingReportIndex.responses?.[status]);
}
const ieltsSpeakingReportIndexResponse = resolveLocalReference(
  ieltsSpeakingReportIndex.responses['200'],
);
assert.equal(
  ieltsSpeakingReportIndexResponse?.headers?.['Cache-Control']?.schema?.const,
  'private, no-store',
  'IELTS Speaking report history must prohibit shared and private caching.',
);
assert.equal(
  getJsonSchema(ieltsSpeakingReportIndexResponse)?.$ref,
  '#/components/schemas/IeltsSpeakingReportIndexPage',
);
assert.ok(
  schemas.IeltsSpeakingReportIndexPage,
  'The root contract must export IeltsSpeakingReportIndexPage.',
);
assert.match(ieltsSpeakingReportIndex.description ?? '', /non-superseded/i);
assert.match(ieltsSpeakingReportIndex.description ?? '', /report kind/i);

const formalReviewHistory = requireOperation('GET /v1/formal-reviews');
const formalReviewHistoryParameters = Object.fromEntries(
  (formalReviewHistory.parameters ?? []).map((parameterValue) => {
    const parameter = resolveLocalReference(parameterValue);
    return [parameter.name, parameter];
  }),
);
const formalReviewCursorPattern = resolveLocalReference(
  formalReviewHistoryParameters.cursor?.schema,
)?.pattern;
const formalReviewHistoryResponse = resolveLocalReference(
  formalReviewHistory.responses?.['200'],
);
const formalReviewHistorySchema = resolveLocalReference(
  getJsonSchema(formalReviewHistoryResponse),
);
const formalReviewNextCursorPattern = resolveLocalReference(
  formalReviewHistorySchema?.properties?.next_cursor,
)?.pattern;
assert.equal(
  formalReviewCursorPattern,
  formalReviewNextCursorPattern,
  'Formal Review request cursor must accept the server next_cursor format.',
);
assert.match(
  'signed_payload.signed_mac',
  new RegExp(formalReviewCursorPattern, 'u'),
);

for (const retiredOperation of [
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
for (const schemaName of [
  'SpeechFeedback',
  'SpeechFeedbackSource',
  'SpeechFeedbackAnchor',
  'FeedbackItem',
  'RetryRequest',
  'RetryTranscriptionCandidate',
  'ConfirmedRetryTurn',
]) {
  assert.ok(schemas[schemaName], `The root contract must export ${schemaName}.`);
}

const assertPrivateNoStoreResponses = (operation, statuses, label) => {
  for (const status of statuses) {
    const response = resolveLocalReference(operation.responses?.[status]);
    assert.ok(response, `${label} must declare ${status}.`);
    assert.equal(
      resolveLocalReference(response.headers?.['Cache-Control'])?.schema?.const,
      'private, no-store',
      `${label} ${status} must prohibit shared and private caching.`,
    );
  }
};

const getSpeechFeedback = requireOperation(
  'GET /v1/speech-feedback/{speech_feedback_id}',
);
assert.equal(getSpeechFeedback.operationId, 'getSpeechFeedback');
assert.deepEqual(
  getSpeechFeedback.security ?? openApi.security,
  bearerSecurity,
  'Speech feedback must derive the Actor from BearerSession.',
);
assertPrivateNoStoreResponses(
  getSpeechFeedback,
  ['200', '401', '404', 'default'],
  'Speech feedback',
);
assert.equal(
  getJsonSchema(resolveLocalReference(getSpeechFeedback.responses['200']))
    ?.$ref,
  '#/components/schemas/SpeechFeedback',
);
assert.equal(schemas.SpeechFeedback?.['x-max-json-bytes'], 524288);
assert.match(getSpeechFeedback.description ?? '', /Another Actor/i);
assert.match(getSpeechFeedback.description ?? '', /must not persist/i);

const createRetryRequest = requireOperation(
  'POST /v1/feedback-items/{feedback_item_id}/retry-requests',
);
assert.equal(createRetryRequest.operationId, 'requestRetry');
assert.equal(
  createRetryRequest.requestBody,
  undefined,
  'Retry creation has no client-controlled body.',
);
assertPrivateNoStoreResponses(
  createRetryRequest,
  ['200', '201', '400', '401', '404', '409', 'default'],
  'Retry creation',
);
const retryCreationParameters = (createRetryRequest.parameters ?? []).map(
  resolveLocalReference,
);
assert.ok(
  retryCreationParameters.some(
    (parameter) =>
      parameter.name === 'Idempotency-Key' &&
      parameter.in === 'header' &&
      parameter.required === true,
  ),
  'Retry creation must require Idempotency-Key.',
);

const getRetryRequest = requireOperation(
  'GET /v1/retry-requests/{retry_request_id}',
);
assert.equal(getRetryRequest.operationId, 'getRetryRequest');
assertPrivateNoStoreResponses(
  getRetryRequest,
  ['200', '401', '404', 'default'],
  'Retry lookup',
);
const retryRequestSchema = resolveLocalReference(schemas.RetryRequest);
assert.equal(
  retryRequestSchema?.properties?.new_turn_status?.const,
  'ANSWERING',
);
assert.equal(
  retryRequestSchema?.properties?.answer_path?.pattern,
  '^/v1/retry-turns/[A-Za-z0-9._~-]{1,128}/transcription-candidates$',
);
const createRetryTurnCommand = resolveLocalReference(
  schemas.CreateRetryTurnCommand,
);
assert.deepEqual(
  createRetryTurnCommand?.required,
  [
    'retry_request_id',
    'practice_session_id',
    'original_turn_id',
    'question_id',
  ],
  'CreateRetryTurnCommand must match the production Review-to-Conversation command.',
);
assert.equal(
  createRetryTurnCommand?.properties?.reason,
  undefined,
  'CreateRetryTurnCommand must not retain the retired smoke-only reason field.',
);
const createRetryTurnResult = resolveLocalReference(
  schemas.CreateRetryTurnResult,
);
for (const requiredProperty of [
  'retry_request_id',
  'question_id',
  'new_turn_id',
  'new_turn_status',
  'answer_path',
]) {
  assert.ok(
    createRetryTurnResult?.required?.includes(requiredProperty),
    `CreateRetryTurnResult must require ${requiredProperty}.`,
  );
}
assert.equal(
  createRetryTurnResult?.properties?.new_turn_status?.const,
  'ANSWERING',
);
assert.equal(
  createRetryTurnResult?.properties?.answer_path?.pattern,
  '^/v1/retry-turns/[A-Za-z0-9._~-]{1,128}/transcription-candidates$',
);

const assertRequiredIdempotencyKey = (operation, label) => {
  const parameters = (operation.parameters ?? []).map(resolveLocalReference);
  assert.ok(
    parameters.some(
      (parameter) =>
        parameter.name === 'Idempotency-Key' &&
        parameter.in === 'header' &&
        parameter.required === true,
    ),
    `${label} must require Idempotency-Key.`,
  );
};

const createRetryCandidate = requireOperation(
  'POST /v1/retry-turns/{retry_turn_id}/transcription-candidates',
);
assert.equal(
  createRetryCandidate.operationId,
  'createRetryTurnTranscriptionCandidate',
);
assertRequiredIdempotencyKey(createRetryCandidate, 'Retry candidate creation');
const retryCandidateRequestBody = resolveLocalReference(
  createRetryCandidate.requestBody,
);
assert.deepEqual(
  Object.keys(retryCandidateRequestBody?.content ?? {}),
  ['audio/wav'],
  'Retry candidate creation must accept raw WAV only.',
);
assert.equal(
  resolveLocalReference(
    retryCandidateRequestBody.content['audio/wav']?.schema,
  )?.format,
  'binary',
);
assertPrivateNoStoreResponses(
  createRetryCandidate,
  ['201', '400', '401', '404', '409', 'default'],
  'Retry candidate creation',
);
assert.equal(
  createRetryCandidate.responses['200'],
  undefined,
  'Retry candidate creation must use 201 for both creation and idempotent restore.',
);
assert.equal(
  getJsonSchema(
    resolveLocalReference(createRetryCandidate.responses['201']),
  )?.$ref,
  '#/components/schemas/RetryTranscriptionCandidate',
);
assert.match(
  resolveLocalReference(createRetryCandidate.responses['201'])?.description ??
    '',
  /newly created.*idempotent replay/is,
);
assert.match(
  createRetryCandidate.description ?? '',
  /derives|resolves/i,
);
assert.match(
  createRetryCandidate.description ?? '',
  /never.*VoiceSessionState/is,
);

const confirmRetryCandidate = requireOperation(
  'POST /v1/retry-turns/{retry_turn_id}/transcription-candidates/{candidate_id}/confirmations',
);
assert.equal(
  confirmRetryCandidate.operationId,
  'confirmRetryTurnTranscriptionCandidate',
);
assert.equal(
  confirmRetryCandidate.requestBody,
  undefined,
  'Retry confirmation must not accept a client-controlled body.',
);
assertRequiredIdempotencyKey(confirmRetryCandidate, 'Retry confirmation');
assertPrivateNoStoreResponses(
  confirmRetryCandidate,
  ['200', '400', '401', '404', '409', 'default'],
  'Retry confirmation',
);
assert.equal(
  getJsonSchema(
    resolveLocalReference(confirmRetryCandidate.responses['200']),
  )?.$ref,
  '#/components/schemas/ConfirmedRetryTurn',
);
assert.doesNotMatch(
  JSON.stringify(confirmRetryCandidate.responses['200']),
  /VoiceSessionState/,
  'Retry confirmation must not reuse VoiceSessionState.',
);

const retryCandidateSchema = resolveLocalReference(
  schemas.RetryTranscriptionCandidate,
);
for (const requiredProperty of [
  'candidate_id',
  'retry_turn_id',
  'retry_request_id',
  'practice_session_id',
  'question_id',
  'respondent_participant_id',
  'transcript_id',
  'evidence_version',
  'transcript',
]) {
  assert.ok(
    retryCandidateSchema?.required?.includes(requiredProperty),
    `RetryTranscriptionCandidate must require ${requiredProperty}.`,
  );
}

const confirmedRetryTurnSchema = resolveLocalReference(
  schemas.ConfirmedRetryTurn,
);
assert.equal(
  confirmedRetryTurnSchema?.properties?.turn_kind?.const,
  'RETRY',
);
assert.equal(
  confirmedRetryTurnSchema?.properties?.turn_status?.const,
  'CONFIRMED',
);
assert.equal(
  confirmedRetryTurnSchema?.properties?.counts_toward_turn_limit?.const,
  false,
);
for (const forbiddenProperty of [
  'effective_turns',
  'session_completed',
  'session_version',
  'turn_limit',
  'current_question',
  'current_turn',
  'turn_history',
  'review_id',
  'speech_feedback_status_url',
]) {
  assert.equal(
    confirmedRetryTurnSchema?.properties?.[forbiddenProperty],
    undefined,
    `ConfirmedRetryTurn must not expose ${forbiddenProperty}.`,
  );
}

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
  ['ConfirmedVoiceTurn', 'speech_feedback_status_url'],
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
const voiceSessionState = resolveLocalReference(schemas.VoiceSessionState);
const voiceQuestion = resolveLocalReference(schemas.VoiceQuestion);
assert.ok(
  voiceQuestion?.required?.includes('question_type'),
  'VoiceQuestion must expose its PRIMARY/FOLLOW_UP classification.',
);
assert.deepEqual(voiceQuestion.properties.question_type.enum, [
  'PRIMARY',
  'FOLLOW_UP',
]);
const confirmedVoiceTurn = resolveLocalReference(schemas.ConfirmedVoiceTurn);
assert.ok(
  confirmedVoiceTurn?.required?.includes(
    'counts_toward_effective_turn_limit',
  ),
  'ConfirmedVoiceTurn must expose whether it advances the displayed round.',
);
assert.ok(
  voiceSessionState?.properties?.turn_history,
  'VoiceSessionState must expose the bounded cold-start Turn history.',
);
assert.equal(
  voiceSessionState.properties.turn_history.maxItems,
  56,
  'Turn history must allow 14 primary Questions with three follow-ups each.',
);
assert.ok(
  !(voiceSessionState.required ?? []).includes('turn_history'),
  'turn_history must remain an optional projection.',
);
assert.match(
  voiceSessionState.properties.turn_history.description ?? '',
  /EFFECTIVE/,
);
const voiceTurnHistoryEntry = resolveLocalReference(
  voiceSessionState.properties.turn_history.items,
);
assert.equal(
  voiceTurnHistoryEntry?.properties?.turn?.$ref,
  '#/components/schemas/ConfirmedVoiceTurn',
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
    `Ordinary ConfirmedVoiceTurn must not expose ${retryOnlyProperty}.`,
  );
}

console.log(
  `Validated ${operations.length} operations: ` +
    `${actualPublicOperations.size} public and ` +
    `${operations.length - actualPublicOperations.size} Bearer-protected.`,
);
