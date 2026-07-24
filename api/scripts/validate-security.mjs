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
  'GET /v1/scenario-definitions/{scenario_definition_id}',
  'GET /v1/scenario-definitions/{scenario_definition_id}/role-definitions',
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
  assert.equal(
    operation.responses['401'].$ref,
    '#/components/responses/Unauthorized',
    `${key} must reuse the common Unauthorized response.`,
  );
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

assert.equal(register.operationId, 'registerUser');
assert.equal(login.operationId, 'loginUser');
assert.equal(logout.operationId, 'logoutCurrentSession');
assert.equal(me.operationId, 'getCurrentUser');
assert.ok(register.requestBody?.required);
assert.ok(login.requestBody?.required);
assert.equal(
  getJsonSchema(register.requestBody)?.$ref,
  '#/components/schemas/RegisterRequest',
);
assert.equal(
  getJsonSchema(login.requestBody)?.$ref,
  '#/components/schemas/LoginRequest',
);
assert.ok(register.responses?.['201']);
assert.ok(register.responses?.['409']);
assert.ok(register.responses?.['429']);
assert.ok(login.responses?.['200']);
assert.ok(login.responses?.['401']);
assert.ok(login.responses?.['429']);
assert.ok(logout.responses?.['204']);
assert.ok(me.responses?.['200']);
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
  assert.deepEqual(sorted(Object.keys(requestSchema?.properties ?? {})), [
    'email',
    'password',
  ]);
  assert.equal(requestSchema?.additionalProperties, false);
}
const passwordSchema = schemas.Password;
assert.equal(passwordSchema?.writeOnly, true);
assert.equal(passwordSchema?.minLength, 15);
assert.equal(passwordSchema?.maxLength, 128);
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
for (const token of ['abc123-._~+/', 'abc123==']) {
  assert.match(token, opaqueSessionTokenPattern);
}
for (const token of ['contains whitespace', 'line\nbreak', 'padding=inside']) {
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
assert.equal(
  responses.Unauthorized?.headers?.['WWW-Authenticate']?.schema?.const,
  'Bearer',
);
assert.ok(responses.TooManyRequests?.headers?.['Retry-After']);
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
assert.equal(
  resolveLocalReference(websocket.responses?.['101'])?.headers?.[
    'Sec-WebSocket-Protocol'
  ]?.schema?.const,
  'speakup.events.v1',
);
assert.equal(websocketParameters.after_sequence?.in, 'query');

console.log(
  `Validated ${operations.length} operations: ` +
    `${actualPublicOperations.size} public and ` +
    `${operations.length - actualPublicOperations.size} Bearer-protected.`,
);
