#!/usr/bin/env node

import { createHash, randomBytes, randomUUID } from "node:crypto";
import {
  constants,
  closeSync,
  fchmodSync,
  lstatSync,
  openSync,
  readFileSync,
  realpathSync,
  writeFileSync,
} from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { isDeepStrictEqual } from "node:util";

export const STAGING_BASE_URL = "https://staging-api.speak-up.top";

export const UAT_MATRIX = Object.freeze([
  Object.freeze({ case_id: "part-1-text", mode: "PART_1", input: "text", timeout_seconds: 480 }),
  Object.freeze({ case_id: "part-1-voice", mode: "PART_1", input: "voice", timeout_seconds: 480 }),
  Object.freeze({ case_id: "part-2-voice", mode: "PART_2", input: "voice", timeout_seconds: 480 }),
  Object.freeze({ case_id: "part-3-voice", mode: "PART_3", input: "voice", timeout_seconds: 480 }),
  Object.freeze({ case_id: "full-mock-text", mode: "FULL_MOCK", input: "text", timeout_seconds: 720 }),
]);

const voiceFixturePath = fileURLToPath(
  new URL("../../server/internal/providers/qianwen/testdata/asr-live-fixture.wav", import.meta.url),
);
const jsonBodyLimit = 2 * 1024 * 1024;
const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const identifierPattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
const optionByMode = Object.freeze({
  FULL_MOCK: "option_ielts_speaking_full_mock",
  PART_1: "option_ielts_speaking_part_1",
  PART_2: "option_ielts_speaking_part_2",
  PART_3: "option_ielts_speaking_part_3",
});

const usage = `Usage:
  node deploy/staging/uat.mjs [--base-url ${STAGING_BASE_URL}]
  UAT_ENV=staging node deploy/staging/uat.mjs --execute \\
    --base-url ${STAGING_BASE_URL} --receipt <private-json-path>`;

export class UatError extends Error {
  constructor(category, message = category) {
    super(message);
    this.name = "UatError";
    this.category = category;
  }
}

class UatSessionError extends UatError {
  constructor(error, sessionReceipt) {
    super(error instanceof UatError ? error.category : "unexpected_error");
    this.sessionReceipt = sessionReceipt;
  }
}

class UatRunError extends UatError {
  constructor(error, receipt) {
    super(error instanceof UatError ? error.category : "unexpected_error");
    this.receipt = receipt;
  }
}

export function parseCli(arguments_) {
  const result = {
    baseUrl: STAGING_BASE_URL,
    execute: false,
    help: false,
    receiptPath: null,
  };
  for (let index = 0; index < arguments_.length; index += 1) {
    const argument = arguments_[index];
    if (argument === "--execute") {
      if (result.execute) throw new UatError("invalid_arguments", usage);
      result.execute = true;
      continue;
    }
    if (argument === "--help") {
      result.help = true;
      continue;
    }
    if (argument !== "--base-url" && argument !== "--receipt") {
      throw new UatError("invalid_arguments", usage);
    }
    const value = arguments_[index + 1];
    if (value === undefined || value.startsWith("--")) {
      throw new UatError("invalid_arguments", usage);
    }
    index += 1;
    if (argument === "--base-url") result.baseUrl = value;
    if (argument === "--receipt") result.receiptPath = value;
  }
  return result;
}

export function validateExecutionBoundary(options, environment = process.env) {
  if (!options.execute) {
    throw new UatError("execute_opt_in_required");
  }
  if (environment.UAT_ENV !== "staging") {
    throw new UatError("staging_environment_required");
  }
  if (options.baseUrl !== STAGING_BASE_URL) {
    throw new UatError("untrusted_base_url");
  }
  if (environment.NODE_TLS_REJECT_UNAUTHORIZED === "0") {
    throw new UatError("tls_verification_disabled");
  }
  if (!options.receiptPath) {
    throw new UatError("receipt_path_required");
  }
  return new URL(`${STAGING_BASE_URL}/`);
}

export function executionPlan(baseUrl = STAGING_BASE_URL) {
  if (baseUrl !== STAGING_BASE_URL) throw new UatError("untrusted_base_url");
  return {
    plan_version: 1,
    dry_run: true,
    environment: "staging",
    base_url: baseUrl,
    network_requests: 0,
    writes: 0,
    sessions: UAT_MATRIX,
  };
}

export function createHttpClient({
  baseUrl,
  fetchImpl = globalThis.fetch,
  timeoutMs = 30_000,
}) {
  const base = new URL(baseUrl);
  if (typeof fetchImpl !== "function" || !Number.isSafeInteger(timeoutMs) || timeoutMs < 1) {
    throw new UatError("invalid_http_client_configuration");
  }
  return async function request(resourcePath, options = {}) {
    if (typeof resourcePath !== "string" || !resourcePath.startsWith("/") || resourcePath.startsWith("//")) {
      throw new UatError("invalid_request_path");
    }
    const url = new URL(resourcePath, base);
    if (url.origin !== base.origin) throw new UatError("untrusted_request_origin");
    let response;
    try {
      response = await fetchImpl(url, {
        ...options,
        redirect: "manual",
        signal: options.signal ?? AbortSignal.timeout(timeoutMs),
      });
    } catch (error) {
      if (error?.name === "TimeoutError" || error?.name === "AbortError") {
        throw new UatError("request_timeout");
      }
      throw new UatError("network_error");
    }
    if (response.status >= 300 && response.status < 400) {
      throw new UatError("redirect_rejected");
    }
    return response;
  };
}

function requireObject(value, category) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new UatError(category);
  }
  return value;
}

function requireArray(value, category, { minimum = 0, maximum = 128 } = {}) {
  if (!Array.isArray(value) || value.length < minimum || value.length > maximum) {
    throw new UatError(category);
  }
  return value;
}

function requireString(value, category, maximumBytes = 65_536) {
  if (
    typeof value !== "string" ||
    value.trim().length === 0 ||
    value.includes("\0") ||
    Buffer.byteLength(value) > maximumBytes
  ) {
    throw new UatError(category);
  }
  return value;
}

function requireUuid(value, category) {
  if (typeof value !== "string" || !uuidPattern.test(value)) {
    throw new UatError(category);
  }
  return value;
}

function requireIdentifier(value, category) {
  if (typeof value !== "string" || !identifierPattern.test(value)) {
    throw new UatError(category);
  }
  return value;
}

function requireInteger(value, category, minimum = 0) {
  if (!Number.isSafeInteger(value) || value < minimum) throw new UatError(category);
  return value;
}

async function readJsonResponse(response, category) {
  const contentLength = Number(response.headers.get("content-length"));
  if (Number.isFinite(contentLength) && contentLength > jsonBodyLimit) {
    throw new UatError(category);
  }
  let bytes;
  try {
    bytes = Buffer.from(await response.arrayBuffer());
  } catch {
    throw new UatError("network_error");
  }
  if (bytes.length > jsonBodyLimit) throw new UatError(category);
  try {
    return JSON.parse(bytes.toString("utf8"));
  } catch {
    throw new UatError(category);
  }
}

async function requestJson(request, resourcePath, {
  method = "GET",
  token,
  body,
  bytes,
  idempotencyKey,
  expectedStatuses = [200],
  retryUncertain = false,
  timeoutMs,
  requireNoStore = false,
  deadlineAt,
  now = Date.now,
} = {}) {
  if (body !== undefined && bytes !== undefined) throw new UatError("invalid_request_body");
  const headers = { accept: "application/json", "cache-control": "no-store" };
  if (token) headers.authorization = `Bearer ${token}`;
  if (idempotencyKey) headers["idempotency-key"] = idempotencyKey;
  if (body !== undefined) headers["content-type"] = "application/json";
  if (bytes !== undefined) headers["content-type"] = "audio/wav";
  const attempts = retryUncertain ? 2 : 1;
  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    try {
      let attemptTimeout = timeoutMs;
      if (deadlineAt !== undefined) {
        const remaining = deadlineAt - now();
        if (!Number.isFinite(remaining) || remaining <= 0) {
          throw new UatError("session_timeout");
        }
        attemptTimeout = Math.min(attemptTimeout ?? remaining, remaining);
      }
      const response = await request(resourcePath, {
        method,
        headers,
        body: body === undefined ? bytes : JSON.stringify(body),
        signal: attemptTimeout === undefined ? undefined : AbortSignal.timeout(attemptTimeout),
      });
      if (!expectedStatuses.includes(response.status)) {
        throw new UatError(`unexpected_http_status_${response.status}`);
      }
      if (
        requireNoStore &&
        (!response.headers.get("cache-control")?.toLowerCase().split(",").map((value) => value.trim()).includes("no-store") ||
          response.headers.get("pragma")?.toLowerCase().trim() !== "no-cache")
      ) {
        throw new UatError("invalid_auth_cache_control");
      }
      if (response.status === 204) return null;
      return await readJsonResponse(response, "invalid_json_response");
    } catch (error) {
      if (
        attempt === attempts ||
        !(error instanceof UatError) ||
        !["network_error", "request_timeout"].includes(error.category)
      ) {
        throw error;
      }
    }
  }
  throw new UatError("network_error");
}

function idempotencyKey(runId, caseId, operation, position = 0) {
  return `uat-${runId}-${caseId}-${operation}-${position}`;
}

function selectQuestions(bank, mode) {
  const root = requireObject(bank, "invalid_question_bank");
  requireString(root.bank_id, "invalid_question_bank", 128);
  const firstPart1 = requireObject(
    requireArray(root.part1_topics, "invalid_question_bank", { minimum: 1 })[0],
    "invalid_question_bank",
  );
  const firstGroup = requireObject(
    requireArray(root.topic_groups, "invalid_question_bank", { minimum: 1 })[0],
    "invalid_question_bank",
  );
  const part1Id = requireString(firstPart1.id, "invalid_question_bank", 128);
  const groupId = requireString(firstGroup.id, "invalid_question_bank", 128);
  if (mode === "PART_1") return { part_1_set_id: part1Id };
  if (mode === "PART_2" || mode === "PART_3") return { topic_group_id: groupId };
  if (mode === "FULL_MOCK") return null;
  throw new UatError("unsupported_uat_mode");
}

function parseAssignment(value, expectedMode, expectedBankId) {
  const assignment = requireObject(value, "invalid_plan_response");
  if (assignment.mode !== expectedMode || assignment.bank_id !== expectedBankId) {
    throw new UatError("plan_mode_mismatch");
  }
  const normalized = {
    bank_id: requireString(assignment.bank_id, "invalid_plan_response", 128),
    season: requireString(assignment.season, "invalid_plan_response", 128),
    mode: assignment.mode,
    parts: [],
  };
  const parts = requireArray(assignment.parts, "invalid_plan_response", { minimum: 1 });
  const expectedParts = expectedMode === "FULL_MOCK"
    ? ["PART_1", "PART_2", "PART_3"]
    : [expectedMode];
  if (
    parts.length !== expectedParts.length ||
    parts.some((rawPart, index) => rawPart?.part !== expectedParts[index])
  ) {
    throw new UatError("plan_mode_mismatch");
  }
  const expectedQuestionCount = parts.reduce((total, rawPart) => {
    const part = requireObject(rawPart, "invalid_plan_response");
    const normalizedPart = {
      part: part.part,
      source_id: requireString(part.source_id, "invalid_plan_response", 128),
      turn_blueprints: requireArray(
        part.turn_blueprints,
        "invalid_plan_response",
        { minimum: 1, maximum: 64 },
      ).map((blueprint) => requireString(blueprint, "invalid_plan_response", 16_384)),
    };
    for (const optional of ["topic_title", "cue_card"]) {
      if (Object.hasOwn(part, optional)) {
        normalizedPart[optional] = requireString(part[optional], "invalid_plan_response", 16_384);
      }
    }
    if (Object.hasOwn(part, "prepared_answers")) {
      normalizedPart.prepared_answers = requireArray(
        part.prepared_answers,
        "invalid_plan_response",
        { maximum: 64 },
      ).map((rawAnswer) => {
        const answer = requireObject(rawAnswer, "invalid_plan_response");
        if (typeof answer.personalized !== "boolean") throw new UatError("invalid_plan_response");
        return {
          question_position: requireInteger(answer.question_position, "invalid_plan_response", 1),
          answer: requireString(answer.answer, "invalid_plan_response", 16_384),
          personalized: answer.personalized,
        };
      });
    }
    normalized.parts.push(normalizedPart);
    return total + normalizedPart.turn_blueprints.length;
  }, 0);
  if (expectedQuestionCount < 1 || expectedQuestionCount > 64) {
    throw new UatError("invalid_plan_response");
  }
  return { assignment: normalized, expectedQuestionCount };
}

function parsePlan(value, expectedMode, expectedBankId) {
  const plan = requireObject(value, "invalid_plan_response");
  const planId = requireUuid(plan.practice_plan_id, "invalid_plan_response");
  const version = requireInteger(plan.version, "invalid_plan_response", 1);
  if (plan.practice_plan_status !== "ready") throw new UatError("plan_not_ready");
  return {
    planId,
    version,
    ...parseAssignment(plan.ielts_assignment, expectedMode, expectedBankId),
  };
}

function parseSessionBootstrap(value, plan, expectedMode) {
  const root = requireObject(value, "invalid_session_response");
  const session = requireObject(root.practice_session, "invalid_session_response");
  const sessionId = requireUuid(session.practice_session_id, "invalid_session_response");
  if (
    session.practice_plan_id !== plan.planId ||
    session.plan_version !== plan.version ||
    session.practice_mode !== expectedMode ||
    session.practice_session_status !== "starting"
  ) {
    throw new UatError("session_binding_mismatch");
  }
  const snapshot = requireObject(root.snapshot, "invalid_session_response");
  const snapshotAssignment = parseAssignment(
    snapshot.ielts_assignment,
    expectedMode,
    plan.assignment.bank_id,
  ).assignment;
  if (
    snapshot.practice_session_id !== sessionId ||
    snapshot.plan_version !== plan.version ||
    snapshot.practice_mode !== expectedMode ||
    !isDeepStrictEqual(snapshotAssignment, plan.assignment)
  ) {
    throw new UatError("session_binding_mismatch");
  }
  return {
    sessionId,
    sessionVersion: requireInteger(session.session_version, "invalid_session_response", 1),
  };
}

function parseInteractionState(value, sessionId, mode, plan) {
  const state = requireObject(value, "invalid_interaction_state");
  const stateAssignment = parseAssignment(
    state.ielts_assignment,
    mode,
    plan.assignment.bank_id,
  ).assignment;
  if (
    state.practice_session_id !== sessionId ||
    state.practice_plan_id !== plan.planId ||
    state.practice_mode !== mode ||
    !isDeepStrictEqual(stateAssignment, plan.assignment)
  ) {
    throw new UatError("interaction_state_mismatch");
  }
  requireInteger(state.effective_turns, "invalid_interaction_state");
  requireInteger(state.turn_limit, "invalid_interaction_state");
  requireInteger(state.session_version, "invalid_interaction_state", 1);
  if (typeof state.session_completed !== "boolean") {
    throw new UatError("invalid_interaction_state");
  }
  const terminal = ["completed", "ended_early"].includes(state.practice_session_status);
  if (
    !["in_progress", "completed", "ended_early"].includes(state.practice_session_status) ||
    state.session_completed !== terminal ||
    state.effective_turns > state.turn_limit ||
    (terminal && Object.hasOwn(state, "current_question")) ||
    (state.effective_turns === 0 && Object.hasOwn(state, "current_turn")) ||
    (state.effective_turns > 0 && !Object.hasOwn(state, "current_turn"))
  ) {
    throw new UatError("invalid_interaction_lifecycle");
  }
  if (!state.session_completed) {
    const question = requireObject(state.current_question, "invalid_interaction_state");
    requireUuid(question.question_id, "invalid_interaction_state");
    if (question.practice_session_id !== sessionId) {
      throw new UatError("interaction_state_mismatch");
    }
    requireString(question.content, "invalid_interaction_state", 16_384);
  }
  return state;
}

function parseTranscriptionCandidate(value, sessionId, questionId) {
  const candidate = requireObject(value, "invalid_transcription_candidate");
  const candidateId = requireUuid(candidate.candidate_id, "invalid_transcription_candidate");
  const respondentParticipantId = requireUuid(
    candidate.respondent_participant_id,
    "invalid_transcription_candidate",
  );
  const transcriptId = requireIdentifier(
    candidate.transcript_id,
    "invalid_transcription_candidate",
  );
  const evidenceVersion = requireInteger(
    candidate.evidence_version,
    "invalid_transcription_candidate",
    1,
  );
  if (candidate.practice_session_id !== sessionId || candidate.question_id !== questionId) {
    throw new UatError("transcription_candidate_mismatch");
  }
  return {
    candidateId,
    respondentParticipantId,
    transcriptId,
    evidenceVersion,
    transcript: requireString(candidate.transcript, "invalid_transcription_candidate", 16_384),
  };
}

function parseConfirmedTurn(state, {
  expectedQuestionId,
  expectedTranscript,
  inputKind,
  candidate,
}) {
  const turn = requireObject(state.current_turn, "invalid_turn_confirmation");
  const turnId = requireUuid(turn.turn_id, "invalid_turn_confirmation");
  const candidateId = requireUuid(turn.candidate_id, "invalid_turn_confirmation");
  const respondentParticipantId = requireUuid(
    turn.respondent_participant_id,
    "invalid_turn_confirmation",
  );
  const evidenceVersion = requireInteger(
    turn.evidence_version,
    "invalid_turn_confirmation",
    1,
  );
  if (
    turn.practice_session_id !== state.practice_session_id ||
    turn.question_id !== expectedQuestionId ||
    turn.answer_text !== expectedTranscript ||
    turn.effective_turns !== state.effective_turns ||
    turn.session_completed !== state.session_completed ||
    turn.counts_toward_effective_turn_limit !== true
  ) {
    throw new UatError("turn_confirmation_mismatch");
  }
  if (inputKind === "voice") {
    if (
      candidateId !== candidate.candidateId ||
      respondentParticipantId !== candidate.respondentParticipantId ||
      evidenceVersion !== candidate.evidenceVersion ||
      !requireUuid(turn.audio_asset_id, "invalid_turn_confirmation")
    ) {
      throw new UatError("turn_confirmation_mismatch");
    }
  } else if (Object.hasOwn(turn, "audio_asset_id")) {
    throw new UatError("turn_confirmation_mismatch");
  }
  return turnId;
}

function sessionRequest(request, deadlineAt, now) {
  return (resourcePath, options = {}) => {
    const remaining = deadlineAt - now();
    if (!Number.isFinite(remaining) || remaining <= 0) {
      throw new UatError("session_timeout");
    }
    const requestedTimeout = options.timeoutMs ?? remaining;
    return requestJson(request, resourcePath, {
      ...options,
      timeoutMs: Math.min(requestedTimeout, remaining),
      deadlineAt,
      now,
    });
  };
}

function validateEvidence(evidence, transcripts, dimensionRefs) {
  const item = requireObject(evidence, "invalid_report_evidence");
  const evidenceRef = requireUuid(item.evidence_ref_id, "invalid_report_evidence");
  const turnId = requireUuid(item.turn_id, "invalid_report_evidence");
  if (evidenceRef !== turnId || !transcripts.has(turnId)) {
    throw new UatError("invalid_report_evidence");
  }
  const start = requireInteger(item.start_utf8_byte, "invalid_report_evidence");
  const end = requireInteger(item.end_utf8_byte, "invalid_report_evidence", 1);
  const excerpt = requireString(item.original_excerpt, "invalid_report_evidence", 16_384);
  const transcript = Buffer.from(transcripts.get(turnId), "utf8");
  if (
    end <= start ||
    end > transcript.length ||
    transcript.subarray(start, end).toString("utf8") !== excerpt
  ) {
    throw new UatError("invalid_report_evidence");
  }
  dimensionRefs.add(evidenceRef);
}

function validateReport(result, expectedMode, answeredQuestions) {
  const report = requireObject(result, "invalid_report");
  if (
    report.schema_version !== "evaluation-report/v2" ||
    report.scene_type !== "IELTS_SPEAKING" ||
    report.practice_experience !== "IELTS_SPEAKING" ||
    report.scene_category !== "IELTS_SPEAKING" ||
    report.practice_mode !== expectedMode
  ) {
    throw new UatError("report_contract_mismatch");
  }
  if (!["PROVISIONAL", "INSUFFICIENT"].includes(report.scoreability_status)) {
    throw new UatError("invalid_report");
  }
  const questions = requireArray(report.questions, "invalid_report", {
    minimum: 1,
    maximum: 128,
  });
  if (questions.length !== answeredQuestions.length) {
    throw new UatError("report_question_count_mismatch");
  }
  const transcripts = new Map();
  for (let index = 0; index < questions.length; index += 1) {
    const actual = requireObject(questions[index], "invalid_report");
    const expected = answeredQuestions[index];
    const answer = requireObject(actual.answer, "invalid_report");
    if (
      actual.position !== index + 1 ||
      actual.question_id !== expected.questionId ||
      actual.text !== expected.questionText ||
      answer.turn_id !== expected.turnId ||
      answer.transcript !== expected.transcript
    ) {
      throw new UatError("report_turn_mismatch");
    }
    transcripts.set(expected.turnId, expected.transcript);
  }
  const dimensions = requireArray(report.dimensions, "invalid_report", {
    minimum: 1,
    maximum: 8,
  });
  const findingIds = new Set();
  const improvementIds = new Map();
  const dimensionKeys = new Set();
  for (const rawDimension of dimensions) {
    const dimension = requireObject(rawDimension, "invalid_report");
    const key = requireIdentifier(dimension.key, "invalid_report");
    if (!dimensionKeys.add(key) || dimension.scale !== "IELTS_BAND_9") {
      throw new UatError("invalid_report_dimension");
    }
    const score = dimension.score;
    if (
      score !== null &&
      (typeof score !== "number" ||
        !Number.isFinite(score) ||
        score < 0 ||
        score > 9 ||
        !Number.isInteger(score * 2))
    ) {
      throw new UatError("invalid_report_dimension");
    }
    for (const ratio of [dimension.coverage, dimension.confidence]) {
      if (typeof ratio !== "number" || !Number.isFinite(ratio) || ratio < 0 || ratio > 1) {
        throw new UatError("invalid_report_dimension");
      }
    }
    if (
      report.scoreability_status === "INSUFFICIENT" &&
      score !== null
    ) {
      throw new UatError("insufficient_report_has_score");
    }
    const reasonCodes = requireArray(dimension.reason_codes, "invalid_report", { maximum: 8 });
    const uniqueReasons = new Set(
      reasonCodes.map((value) => requireIdentifier(value, "invalid_report")),
    );
    if (uniqueReasons.size !== reasonCodes.length) throw new UatError("invalid_report_dimension");
    const evidenceRefs = requireArray(dimension.evidence_ref_ids, "invalid_report", {
      maximum: 128,
    }).map((value) => requireUuid(value, "invalid_report"));
    const declaredRefs = new Set(
      evidenceRefs,
    );
    if (declaredRefs.size !== evidenceRefs.length) throw new UatError("invalid_report_evidence_refs");
    const actualRefs = new Set();
    const improvements = new Set();
    for (const collectionName of ["strengths", "improvements", "recommended_examples"]) {
      const findings = requireArray(dimension[collectionName], "invalid_report", { maximum: 5 });
      for (const rawFinding of findings) {
        const finding = requireObject(rawFinding, "invalid_report");
        const findingId = requireIdentifier(finding.finding_id, "invalid_report");
        if (findingIds.has(findingId)) throw new UatError("invalid_report");
        findingIds.add(findingId);
        if (collectionName === "improvements") improvements.add(findingId);
        for (const evidence of requireArray(finding.evidence, "invalid_report", { maximum: 8 })) {
          validateEvidence(evidence, transcripts, actualRefs);
        }
      }
    }
    if (
      declaredRefs.size !== actualRefs.size ||
      [...declaredRefs].some((reference) => !actualRefs.has(reference))
    ) {
      throw new UatError("invalid_report_evidence_refs");
    }
    improvementIds.set(key, improvements);
  }
  const actions = requireArray(report.priority_actions, "invalid_report", { maximum: 5 });
  if (report.scoreability_status === "INSUFFICIENT" && actions.length !== 0) {
    throw new UatError("insufficient_report_has_priority_actions");
  }
  for (const rawAction of actions) {
    const action = requireObject(rawAction, "invalid_report");
    if (!improvementIds.get(action.dimension_key)?.has(action.finding_id)) {
      throw new UatError("invalid_report_priority_action");
    }
  }
  return {
    schemaVersion: report.schema_version,
    scoreabilityStatus: report.scoreability_status,
  };
}

function parseEvaluation(value, sessionId) {
  const evaluation = requireObject(value, "invalid_evaluation_response");
  const evaluationId = requireUuid(evaluation.evaluation_id, "invalid_evaluation_response");
  if (
    evaluation.kind !== "SESSION_REPORT" ||
    evaluation.source_id !== sessionId ||
    evaluation.context_id !== sessionId ||
    !["QUEUED", "RUNNING", "READY", "FAILED"].includes(evaluation.status)
  ) {
    throw new UatError("evaluation_binding_mismatch");
  }
  if (
    requireArray(evaluation.feedback_items, "invalid_evaluation_response", { maximum: 32 }).length !== 0
  ) {
    throw new UatError("invalid_evaluation_response");
  }
  const hasResult = Object.hasOwn(evaluation, "result");
  const hasError = Object.hasOwn(evaluation, "error");
  if (
    (["QUEUED", "RUNNING"].includes(evaluation.status) && (hasResult || hasError)) ||
    (evaluation.status === "READY" && (!hasResult || hasError)) ||
    (evaluation.status === "FAILED" && (!hasError || hasResult))
  ) {
    throw new UatError("invalid_evaluation_state");
  }
  if (evaluation.status === "FAILED") {
    const failure = requireObject(evaluation.error, "invalid_evaluation_state");
    requireIdentifier(failure.code, "invalid_evaluation_state");
    requireString(failure.message, "invalid_evaluation_state", 2048);
    if (typeof failure.retryable !== "boolean") {
      throw new UatError("invalid_evaluation_state");
    }
  }
  return { evaluation, evaluationId };
}

async function pollEvaluation({
  request,
  token,
  sessionId,
  mode,
  answeredQuestions,
  deadlineAt,
  pollIntervalMs,
  now,
  sleep,
  onEvaluationState,
}) {
  let stableEvaluationId = null;
  while (now() < deadlineAt) {
    const value = await request(
      `/v1/practice-sessions/${encodeURIComponent(sessionId)}/evaluation`,
      { token },
    );
    const { evaluation, evaluationId } = parseEvaluation(value, sessionId);
    onEvaluationState(evaluationId, evaluation.status);
    if (stableEvaluationId !== null && stableEvaluationId !== evaluationId) {
      throw new UatError("evaluation_identity_changed");
    }
    stableEvaluationId = evaluationId;
    if (evaluation.status === "FAILED") throw new UatError("evaluation_failed");
    if (evaluation.status === "READY") {
      return {
        evaluationId,
        ...validateReport(evaluation.result, mode, answeredQuestions),
      };
    }
    const remaining = deadlineAt - now();
    if (remaining <= 0) break;
    await sleep(Math.min(pollIntervalMs, remaining));
  }
  throw new UatError("session_timeout");
}

async function runSessionInternal({
  request,
  token,
  bank,
  entry,
  runId,
  voiceBytes,
  now,
  sleep,
  pollIntervalMs,
  startedAt,
  progress,
}) {
  const deadlineAt = startedAt + entry.timeout_seconds * 1000;
  const requestWithinSession = sessionRequest(request, deadlineAt, now);
  const selection = selectQuestions(bank, entry.mode);
  const planValue = await requestWithinSession("/v1/practice-plans", {
    method: "POST",
    token,
    idempotencyKey: idempotencyKey(runId, entry.case_id, "plan"),
    retryUncertain: true,
    expectedStatuses: [201],
    body: {
      background_summary: "Staging release UAT.",
      scene_id: "scn_ielts_speaking",
      scene_version: 1,
      selected_role_ids: ["role_ielts_speaking_examiner"],
      practice_option_id: optionByMode[entry.mode],
      ...(selection === null ? {} : { ielts_selection: selection }),
    },
  });
  const plan = parsePlan(planValue, entry.mode, bank.bank_id);
  progress.resource_refs.plan = redactResource(plan.planId);
  const bootstrap = await requestWithinSession(
    `/v1/practice-plans/${encodeURIComponent(plan.planId)}/practice-sessions`,
    {
      method: "POST",
      token,
      idempotencyKey: idempotencyKey(runId, entry.case_id, "session"),
      retryUncertain: true,
      expectedStatuses: [201],
      body: { expected_plan_version: plan.version },
    },
  );
  const session = parseSessionBootstrap(bootstrap, plan, entry.mode);
  const { sessionId } = session;
  progress.resource_refs.session = redactResource(sessionId);
  let state = parseInteractionState(
    await requestWithinSession(
      `/v1/practice-sessions/${encodeURIComponent(sessionId)}/activation`,
      {
        method: "POST",
        token,
        idempotencyKey: idempotencyKey(runId, entry.case_id, "activate"),
        retryUncertain: true,
      },
    ),
    sessionId,
    entry.mode,
    plan,
  );
  if (
    state.practice_session_status !== "in_progress" ||
    state.session_version <= session.sessionVersion ||
    state.effective_turns !== 0 ||
    state.session_completed ||
    Object.hasOwn(state, "current_turn")
  ) {
    throw new UatError("invalid_interaction_lifecycle");
  }
  const turnLimit = state.turn_limit;
  if (state.turn_limit !== plan.expectedQuestionCount) {
    throw new UatError("session_question_count_mismatch");
  }
  const answeredQuestions = [];
  const seenQuestions = new Set();
  while (!state.session_completed) {
    if (answeredQuestions.length >= 64) throw new UatError("session_turn_limit_exceeded");
    const question = state.current_question;
    const previousSessionVersion = state.session_version;
    const questionId = question.question_id;
    if (!seenQuestions.add(questionId)) throw new UatError("repeated_current_question");
    let transcript;
    if (entry.input === "voice") {
      const candidate = requireObject(
        await requestWithinSession(
          `/v1/practice-sessions/${encodeURIComponent(sessionId)}/questions/${encodeURIComponent(questionId)}/transcription-candidates`,
          {
            method: "POST",
            token,
            bytes: voiceBytes,
            idempotencyKey: idempotencyKey(
              runId,
              entry.case_id,
              "transcribe",
              answeredQuestions.length + 1,
            ),
            retryUncertain: true,
            expectedStatuses: [201],
            timeoutMs: 540_000,
          },
        ),
        "invalid_transcription_candidate",
      );
      const candidateBinding = parseTranscriptionCandidate(candidate, sessionId, questionId);
      transcript = candidateBinding.transcript;
      state = parseInteractionState(
        await requestWithinSession(
          `/v1/transcription-candidates/${encodeURIComponent(candidateBinding.candidateId)}/confirmations`,
          {
            method: "POST",
            token,
            idempotencyKey: idempotencyKey(
              runId,
              entry.case_id,
              "confirm",
              answeredQuestions.length + 1,
            ),
            retryUncertain: true,
          },
        ),
        sessionId,
        entry.mode,
        plan,
      );
      const turnId = parseConfirmedTurn(state, {
        expectedQuestionId: questionId,
        expectedTranscript: transcript,
        inputKind: entry.input,
        candidate: candidateBinding,
      });
      answeredQuestions.push({ questionId, questionText: question.content, turnId, transcript });
    } else {
      transcript = "I can explain this clearly with a concrete example from my experience.";
      state = parseInteractionState(
        await requestWithinSession(
          `/v1/practice-sessions/${encodeURIComponent(sessionId)}/questions/${encodeURIComponent(questionId)}/text-answers`,
          {
            method: "POST",
            token,
            body: { answer_text: transcript },
            idempotencyKey: idempotencyKey(
              runId,
              entry.case_id,
              "text",
              answeredQuestions.length + 1,
            ),
            retryUncertain: true,
          },
        ),
        sessionId,
        entry.mode,
        plan,
      );
      const turnId = parseConfirmedTurn(state, {
        expectedQuestionId: questionId,
        expectedTranscript: transcript,
        inputKind: entry.input,
      });
      answeredQuestions.push({ questionId, questionText: question.content, turnId, transcript });
    }
    const expectedEffectiveTurns = answeredQuestions.length;
    const expectedCompleted = expectedEffectiveTurns === turnLimit;
    if (
      state.session_version <= previousSessionVersion ||
      state.turn_limit !== turnLimit ||
      state.effective_turns !== expectedEffectiveTurns ||
      state.session_completed !== expectedCompleted ||
      state.practice_session_status !== (expectedCompleted ? "completed" : "in_progress")
    ) {
      throw new UatError("invalid_interaction_lifecycle");
    }
  }
  if (answeredQuestions.length !== plan.expectedQuestionCount) {
    throw new UatError("session_question_count_mismatch");
  }
  const evaluation = await pollEvaluation({
    request: requestWithinSession,
    token,
    sessionId,
    mode: entry.mode,
    answeredQuestions,
    deadlineAt,
    pollIntervalMs,
    now,
    sleep,
    onEvaluationState: (evaluationId, status) => {
      progress.resource_refs.evaluation = redactResource(evaluationId);
      progress.evaluation_status = status;
    },
  });
  return {
    case_id: entry.case_id,
    mode: entry.mode,
    input_kind: entry.input,
    duration_ms: now() - startedAt,
    evaluation_status: "READY",
    schema_version: evaluation.schemaVersion,
    scoreability_status: evaluation.scoreabilityStatus,
    error_category: null,
    resource_refs: {
      ...progress.resource_refs,
    },
  };
}

async function runSession(arguments_) {
  const startedAt = arguments_.now();
  const progress = {
    case_id: arguments_.entry.case_id,
    mode: arguments_.entry.mode,
    input_kind: arguments_.entry.input,
    resource_refs: {},
  };
  try {
    return await runSessionInternal({ ...arguments_, startedAt, progress });
  } catch (error) {
    throw new UatSessionError(error, {
      ...progress,
      duration_ms: arguments_.now() - startedAt,
      evaluation_status: progress.evaluation_status ?? "NOT_REACHED",
      schema_version: null,
      scoreability_status: null,
      error_category: error instanceof UatError ? error.category : "unexpected_error",
    });
  }
}

function redactResource(value) {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}

export async function runStagingUat({
  baseUrl = STAGING_BASE_URL,
  fetchImpl = globalThis.fetch,
  now = Date.now,
  sleep = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)),
  pollIntervalMs = 5_000,
  matrix = UAT_MATRIX,
  voiceBytes = readFileSync(voiceFixturePath),
} = {}) {
  const startedAt = new Date(now()).toISOString();
  const runId = randomUUID();
  const request = createHttpClient({ baseUrl, fetchImpl });
  const sessions = [];
  try {
    const health = requireObject(
      await requestJson(request, "/health"),
      "invalid_health_response",
    );
    if (health.status !== "ok") throw new UatError("staging_not_ready");
    const readiness = requireObject(
      await requestJson(request, "/readyz"),
      "invalid_readiness_response",
    );
    if (
      readiness.status !== "ready" ||
      requireObject(readiness.checks, "invalid_readiness_response").database !== "ready"
    ) {
      throw new UatError("staging_not_ready");
    }
    const emailNonce = randomBytes(18).toString("hex");
    const email = `speakup-uat-${emailNonce}@example.invalid`;
    const password = `Uat-${randomBytes(24).toString("base64url")}`;
    await requestJson(request, "/v1/auth/register", {
      method: "POST",
      expectedStatuses: [201],
      body: { email, password },
    });
    const login = requireObject(
      await requestJson(request, "/v1/auth/login", {
        method: "POST",
        body: { email, password },
        requireNoStore: true,
      }),
      "invalid_login_response",
    );
    const token = requireString(login.session_token, "invalid_login_response", 2048);
    if (!token.startsWith("sess_") || login.token_type !== "Bearer") {
      throw new UatError("invalid_login_response");
    }
    const bank = await requestJson(request, "/v1/ielts-speaking/question-bank", { token });
    if (requireObject(bank, "invalid_question_bank").schema_version !== 4) {
      throw new UatError("invalid_question_bank");
    }
    for (const entry of matrix) {
      sessions.push(
        await runSession({
          request,
          token,
          bank,
          entry,
          runId,
          voiceBytes,
          now,
          sleep,
          pollIntervalMs,
        }),
      );
    }
    await requestJson(request, "/v1/auth/logout", {
      method: "POST",
      token,
      expectedStatuses: [204],
    });
    return {
      receipt_version: 1,
      run_id: runId,
      environment: "staging",
      base_host: new URL(baseUrl).hostname,
      started_at: startedAt,
      completed_at: new Date(now()).toISOString(),
      outcome: "passed",
      sessions,
      error_category: null,
    };
  } catch (error) {
    if (error instanceof UatSessionError) sessions.push(error.sessionReceipt);
    throw new UatRunError(error, {
      receipt_version: 1,
      run_id: runId,
      environment: "staging",
      base_host: new URL(baseUrl).hostname,
      started_at: startedAt,
      completed_at: new Date(now()).toISOString(),
      outcome: "failed",
      sessions,
      error_category: error instanceof UatError ? error.category : "unexpected_error",
    });
  }
}

const sensitiveKeyPattern = /(?:password|token|email|question|answer|transcript|audio|provider|payload|secret|access.?key)/i;
const credentialValuePattern = /(?:sess_[A-Za-z0-9._~+/=-]+|[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,})/;

export function assertReceiptIsRedacted(value, trail = "receipt") {
  if (Array.isArray(value)) {
    value.forEach((entry, index) => assertReceiptIsRedacted(entry, `${trail}[${index}]`));
    return;
  }
  if (value !== null && typeof value === "object") {
    for (const [key, entry] of Object.entries(value)) {
      if (sensitiveKeyPattern.test(key)) {
        throw new UatError("receipt_contains_sensitive_field", `${trail}.${key}`);
      }
      assertReceiptIsRedacted(entry, `${trail}.${key}`);
    }
    return;
  }
  if (typeof value === "string" && credentialValuePattern.test(value)) {
    throw new UatError("receipt_contains_sensitive_value", trail);
  }
}

function prepareReceiptTarget(receiptPath) {
  const resolved = path.resolve(receiptPath);
  try {
    lstatSync(resolved);
    throw new UatError("receipt_already_exists");
  } catch (error) {
    if (error instanceof UatError) throw error;
    if (error?.code !== "ENOENT") throw error;
  }
  const parent = path.dirname(resolved);
  let status;
  try {
    status = lstatSync(parent);
  } catch (error) {
    if (error?.code === "ENOENT") throw new UatError("receipt_directory_required");
    throw error;
  }
  const wrongOwner = typeof process.getuid === "function" && status.uid !== process.getuid();
  if (
    status.isSymbolicLink() ||
    !status.isDirectory() ||
    (status.mode & 0o777) !== 0o700 ||
    wrongOwner ||
    realpathSync(parent) !== parent
  ) {
    throw new UatError("receipt_directory_not_private");
  }
  return resolved;
}

export function writeReceipt(receiptPath, receipt) {
  assertReceiptIsRedacted(receipt);
  const resolved = prepareReceiptTarget(receiptPath);
  let descriptor;
  try {
    descriptor = openSync(
      resolved,
      constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY,
      0o600,
    );
    fchmodSync(descriptor, 0o600);
    writeFileSync(descriptor, `${JSON.stringify(receipt, null, 2)}\n`, "utf8");
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
  }
}

export async function runCli({
  arguments_: argumentsValue,
  environment = process.env,
  output = process.stdout,
  executeUat,
}) {
  const options = parseCli(argumentsValue);
  if (options.help) {
    output.write(`${usage}\n`);
    return { kind: "help" };
  }
  if (!options.execute) {
    const plan = executionPlan(options.baseUrl);
    output.write(`${JSON.stringify(plan, null, 2)}\n`);
    return { kind: "dry-run", plan };
  }
  validateExecutionBoundary(options, environment);
  prepareReceiptTarget(options.receiptPath);
  if (typeof executeUat !== "function") {
    throw new UatError("uat_executor_required");
  }
  let receipt;
  try {
    receipt = await executeUat(options);
  } catch (error) {
    writeReceipt(options.receiptPath, error instanceof UatRunError ? error.receipt : {
      receipt_version: 1,
      run_id: randomUUID(),
      environment: "staging",
      base_host: new URL(options.baseUrl).hostname,
      completed_at: new Date().toISOString(),
      outcome: "failed",
      sessions: [],
      error_category: error instanceof UatError ? error.category : "unexpected_error",
    });
    throw error;
  }
  writeReceipt(options.receiptPath, receipt);
  output.write(`${JSON.stringify({ outcome: receipt.outcome, receipt: path.resolve(options.receiptPath) })}\n`);
  return { kind: "executed", receipt };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  runCli({
    arguments_: process.argv.slice(2),
    executeUat: (options) => runStagingUat({ baseUrl: options.baseUrl }),
  }).catch((error) => {
    const category = error instanceof UatError ? error.category : "unexpected_error";
    process.stderr.write(`Staging UAT failed: ${category}\n`);
    process.exitCode = 1;
  });
}
