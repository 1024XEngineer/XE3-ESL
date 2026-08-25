import assert from "node:assert/strict";
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  statSync,
  symlinkSync,
} from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  STAGING_BASE_URL,
  UAT_MATRIX,
  UatError,
  createHttpClient,
  runCli,
  runStagingUat,
  validateExecutionBoundary,
  writeReceipt,
} from "./uat.mjs";

function outputBuffer() {
  let value = "";
  return {
    output: { write: (chunk) => { value += chunk; } },
    read: () => value,
  };
}

function uuid(value) {
  return `00000000-0000-4000-8000-${value.toString(16).padStart(12, "0")}`;
}

async function requestBody(request) {
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  return Buffer.concat(chunks);
}

function json(response, status, value, headers = {}) {
  const body = Buffer.from(JSON.stringify(value));
  response.writeHead(status, {
    "content-type": "application/json",
    "content-length": body.length,
    ...headers,
  });
  response.end(body);
}

async function createUatServer(behavior = "ready") {
  let sequence = 1;
  const plans = new Map();
  const sessions = new Map();
  const candidates = new Map();
  const counts = { plans: 0, requests: 0 };
  const nextId = () => uuid(sequence++);
  const modeByOption = {
    option_ielts_speaking_part_1: "PART_1",
    option_ielts_speaking_part_2: "PART_2",
    option_ielts_speaking_part_3: "PART_3",
    option_ielts_speaking_full_mock: "FULL_MOCK",
  };
  const totalByMode = { PART_1: 2, PART_2: 1, PART_3: 2, FULL_MOCK: 3 };

  function interaction(session) {
    const completed = session.answers.length === session.questions.length;
    return {
      practice_session_id: session.id,
      practice_plan_id: behavior === "bad-state-plan" ? nextId() : session.planId,
      practice_mode: session.mode,
      ielts_assignment: behavior === "bad-state-assignment"
        ? {
            ...session.assignment,
            parts: session.assignment.parts.map((part, index) => index === 0
              ? { ...part, source_id: "mutated-state-source" }
              : part),
          }
        : session.assignment,
      practice_session_status: behavior === "bad-lifecycle" && completed
        ? "in_progress"
        : completed ? "completed" : "in_progress",
      session_version: session.answers.length + 2,
      effective_turns: session.answers.length,
      turn_limit: session.questions.length,
      session_completed: completed,
      ...(completed
        ? {}
        : { current_question: session.questions[session.answers.length] }),
      ...(session.answers.length === 0
        ? {}
        : { current_turn: session.answers.at(-1) }),
    };
  }

  function advance(session, questionId, transcript, binding = {}) {
    assert.equal(session.questions[session.answers.length].question_id, questionId);
    const nextEffectiveTurns = session.answers.length + 1;
    session.answers.push({
      turn_id: nextId(),
      practice_session_id: session.id,
      question_id: questionId,
      respondent_participant_id: binding.respondentParticipantId ?? nextId(),
      candidate_id: binding.candidateId ?? nextId(),
      answer_text: transcript,
      evidence_version: binding.evidenceVersion ?? 1,
      effective_turns: nextEffectiveTurns,
      counts_toward_effective_turn_limit: true,
      session_completed: nextEffectiveTurns === session.questions.length,
      ...(binding.audioAssetId === undefined ? {} : { audio_asset_id: binding.audioAssetId }),
    });
    return interaction(session);
  }

  function report(session) {
    const first = session.answers[0];
    const transcript = first.answer_text;
    const excerpt = transcript.slice(0, 5);
    const invalidEnd = behavior === "invalid-evidence" ? 6 : Buffer.byteLength(excerpt);
    const insufficient = behavior === "insufficient-score";
    const finding = {
      finding_id: "finding-1",
      message: "Use a more specific supporting example.",
      suggestion: "Add one measurable result.",
      evidence: [{
        evidence_ref_id: first.turn_id,
        turn_id: first.turn_id,
        start_utf8_byte: 0,
        end_utf8_byte: invalidEnd,
        original_excerpt: excerpt,
      }],
    };
    return {
      schema_version: "evaluation-report/v2",
      scene_type: "IELTS_SPEAKING",
      practice_experience: "IELTS_SPEAKING",
      scene_category: "IELTS_SPEAKING",
      practice_mode: session.mode,
      scoreability_status: insufficient ? "INSUFFICIENT" : "PROVISIONAL",
      summary: "Staging UAT report.",
      questions: session.questions.map((question, index) => ({
        question_id: question.question_id,
        position: index + 1,
        text: question.content,
        answer: {
          turn_id: session.answers[index].turn_id,
          transcript: session.answers[index].answer_text,
        },
      })),
      dimensions: [{
        key: "FLUENCY_COHERENCE",
        score: insufficient ? 6.5 : behavior === "invalid-dimension" ? 6.3 : 6,
        scale: "IELTS_BAND_9",
        coverage: 1,
        confidence: 0.8,
        reason_codes: [],
        evidence_ref_ids: [first.turn_id],
        strengths: [],
        improvements: [finding],
        recommended_examples: [],
      }],
      priority_actions: insufficient
        ? []
        : [{ dimension_key: "FLUENCY_COHERENCE", finding_id: "finding-1" }],
    };
  }

  const server = http.createServer(async (request, response) => {
    counts.requests += 1;
    const url = new URL(request.url, "http://127.0.0.1");
    if (request.method === "GET" && url.pathname === "/health") {
      json(response, 200, {
        status: behavior === "bad-health" ? "degraded" : "ok",
        modules: ["practice"],
      });
      return;
    }
    if (request.method === "GET" && url.pathname === "/readyz") {
      json(response, 200, {
        status: behavior === "bad-readiness" ? "unavailable" : "ready",
        checks: {
          database: behavior === "bad-readiness" ? "unavailable" : "ready",
        },
      });
      return;
    }
    if (request.method === "POST" && url.pathname === "/v1/auth/register") {
      await requestBody(request);
      json(response, 201, { user_id: nextId(), email: "redacted@example.invalid" });
      return;
    }
    if (request.method === "POST" && url.pathname === "/v1/auth/login") {
      await requestBody(request);
      json(
        response,
        200,
        { session_token: "sess_test-only-token", token_type: "Bearer" },
        behavior === "bad-login-cache"
          ? { "cache-control": "public, max-age=60" }
          : { "cache-control": "no-store", pragma: "no-cache" },
      );
      return;
    }
    if (request.method === "POST" && url.pathname === "/v1/auth/logout") {
      response.writeHead(204).end();
      return;
    }
    if (request.method === "GET" && url.pathname === "/v1/ielts-speaking/question-bank") {
      json(response, 200, {
        schema_version: behavior === "bad-bank-version" ? 3 : 4,
        bank_id: "bank-current",
        part1_topics: [{ id: "part-1-current" }],
        topic_groups: [{ id: "topic-current" }],
      });
      return;
    }
    if (request.method === "POST" && url.pathname === "/v1/practice-plans") {
      const body = JSON.parse((await requestBody(request)).toString("utf8"));
      const mode = modeByOption[body.practice_option_id];
      assert.ok(mode);
      assert.equal(body.scene_id, "scn_ielts_speaking");
      if (mode === "FULL_MOCK") {
        assert.equal(Object.hasOwn(body, "ielts_selection"), false);
      } else {
        assert.equal(body.ielts_selection.part_1_set_id, mode === "PART_1" ? "part-1-current" : undefined);
        assert.equal(body.ielts_selection.topic_group_id, mode === "PART_1" ? undefined : "topic-current");
      }
      const planId = nextId();
      const total = totalByMode[mode];
      const partModes = mode === "FULL_MOCK" ? ["PART_1", "PART_2", "PART_3"] : [mode];
      const assignment = {
        bank_id: behavior === "bad-assignment-bank" ? "stale-bank" : "bank-current",
        season: "2026-08",
        mode,
        parts: (behavior === "bad-assignment-parts" ? ["PART_3"] : partModes).map((part) => ({
          part,
          source_id: `${part.toLowerCase()}-source-current`,
          turn_blueprints: Array.from(
            { length: mode === "FULL_MOCK" ? 1 : total },
            (_, index) => `${part}-question-${index + 1}`,
          ),
        })),
      };
      plans.set(planId, { mode, total, assignment });
      counts.plans += 1;
      json(response, 201, {
        practice_plan_id: planId,
        version: 1,
        practice_plan_status: "ready",
        ielts_assignment: assignment,
      });
      return;
    }
    const sessionCreate = url.pathname.match(/^\/v1\/practice-plans\/([^/]+)\/practice-sessions$/);
    if (request.method === "POST" && sessionCreate) {
      await requestBody(request);
      const planId = sessionCreate[1];
      const plan = plans.get(planId);
      assert.ok(plan);
      const sessionId = nextId();
      const questions = Array.from({ length: plan.total }, (_, index) => ({
        question_id: nextId(),
        practice_session_id: sessionId,
        content: `Question ${index + 1} for ${plan.mode}`,
      }));
      sessions.set(sessionId, {
        id: sessionId,
        planId,
        mode: plan.mode,
        questions,
        answers: [],
        ordinal: sessions.size + 1,
        assignment: plan.assignment,
      });
      json(response, 201, {
        practice_session: {
          practice_session_id: sessionId,
          practice_plan_id: planId,
          plan_version: 1,
          practice_mode: plan.mode,
          practice_session_status: "starting",
          session_version: 1,
        },
        snapshot: {
          practice_session_id: sessionId,
          plan_version: 1,
          practice_mode: plan.mode,
          ielts_assignment: behavior === "bad-snapshot-freeze"
            ? {
                ...plan.assignment,
                parts: plan.assignment.parts.map((part, index) => index === 0
                  ? { ...part, source_id: "mutated-source" }
                  : part),
              }
            : plan.assignment,
        },
      });
      return;
    }
    const activation = url.pathname.match(/^\/v1\/practice-sessions\/([^/]+)\/activation$/);
    if (request.method === "POST" && activation) {
      json(response, 200, interaction(sessions.get(activation[1])));
      return;
    }
    const textAnswer = url.pathname.match(/^\/v1\/practice-sessions\/([^/]+)\/questions\/([^/]+)\/text-answers$/);
    if (request.method === "POST" && textAnswer) {
      const body = JSON.parse((await requestBody(request)).toString("utf8"));
      json(response, 200, advance(sessions.get(textAnswer[1]), textAnswer[2], body.answer_text));
      return;
    }
    const transcription = url.pathname.match(/^\/v1\/practice-sessions\/([^/]+)\/questions\/([^/]+)\/transcription-candidates$/);
    if (request.method === "POST" && transcription) {
      assert.ok((await requestBody(request)).length > 0);
      const candidateId = nextId();
      const respondentParticipantId = nextId();
      const transcriptId = `fun-asr-safe-${sequence++}`;
      candidates.set(candidateId, {
        sessionId: transcription[1],
        questionId: transcription[2],
        transcript: "Voice fixture answer.",
        respondentParticipantId,
        transcriptId,
        evidenceVersion: 1,
      });
      json(response, 201, {
        candidate_id: candidateId,
        practice_session_id: behavior === "bad-candidate-binding" ? nextId() : transcription[1],
        question_id: transcription[2],
        respondent_participant_id: respondentParticipantId,
        transcript_id: transcriptId,
        evidence_version: 1,
        transcript: "Voice fixture answer.",
      });
      return;
    }
    const confirmation = url.pathname.match(/^\/v1\/transcription-candidates\/([^/]+)\/confirmations$/);
    if (request.method === "POST" && confirmation) {
      const candidate = candidates.get(confirmation[1]);
      json(response, 200, advance(
        sessions.get(candidate.sessionId),
        candidate.questionId,
        candidate.transcript,
        {
          candidateId: behavior === "bad-turn-binding" ? nextId() : confirmation[1],
          respondentParticipantId: candidate.respondentParticipantId,
          evidenceVersion: candidate.evidenceVersion,
          audioAssetId: nextId(),
        },
      ));
      return;
    }
    const evaluation = url.pathname.match(/^\/v1\/practice-sessions\/([^/]+)\/evaluation$/);
    if (request.method === "GET" && evaluation) {
      const session = sessions.get(evaluation[1]);
      session.evaluationId ??= nextId();
      const base = {
        evaluation_id: session.evaluationId,
        kind: "SESSION_REPORT",
        source_id: session.id,
        context_id: session.id,
        feedback_items: [],
      };
      if (behavior === "failed" || (behavior === "fail-second" && session.ordinal === 2)) {
        json(response, 200, { ...base, status: "FAILED", error: { code: "provider_response", retryable: false, message: "failed" } });
      } else if (behavior === "queued-with-result") {
        json(response, 200, { ...base, status: "RUNNING", result: report(session) });
      } else if (behavior === "timeout") {
        json(response, 200, { ...base, status: "RUNNING" });
      } else {
        json(response, 200, { ...base, status: "READY", result: report(session) });
      }
      return;
    }
    response.writeHead(404).end();
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address();
  return {
    baseUrl: `http://127.0.0.1:${port}`,
    close: () => new Promise((resolve) => server.close(resolve)),
    counts,
  };
}

test("default dry-run prints the five-session plan without invoking the network executor", async () => {
  const buffer = outputBuffer();
  let calls = 0;
  const result = await runCli({
    arguments_: [],
    environment: {},
    output: buffer.output,
    executeUat: async () => { calls += 1; },
  });
  assert.equal(result.kind, "dry-run");
  assert.equal(calls, 0);
  const plan = JSON.parse(buffer.read());
  assert.equal(plan.network_requests, 0);
  assert.equal(plan.writes, 0);
  assert.deepEqual(
    plan.sessions.map(({ mode, input }) => `${mode}:${input}`),
    ["PART_1:text", "PART_1:voice", "PART_2:voice", "PART_3:voice", "FULL_MOCK:text"],
  );
  await assert.rejects(
    runCli({
      arguments_: ["--base-url", "https://api.speak-up.top"],
      environment: {},
      output: buffer.output,
      executeUat: async () => { calls += 1; },
    }),
    (error) => error instanceof UatError && error.category === "untrusted_base_url",
  );
  assert.equal(calls, 0);
});

test("execution boundary fails closed unless every Staging opt-in is exact", () => {
  const valid = {
    execute: true,
    baseUrl: STAGING_BASE_URL,
    receiptPath: "/private/tmp/staging-uat-receipt.json",
  };
  assert.throws(
    () => validateExecutionBoundary({ ...valid, execute: false }, { UAT_ENV: "staging" }),
    (error) => error instanceof UatError && error.category === "execute_opt_in_required",
  );
  for (const baseUrl of [
    "https://api.speak-up.top",
    "http://staging-api.speak-up.top",
    "https://149.71.241.71",
    "http://localhost:8080",
    `${STAGING_BASE_URL}/`,
  ]) {
    assert.throws(
      () => validateExecutionBoundary({ ...valid, baseUrl }, { UAT_ENV: "staging" }),
      (error) => error instanceof UatError && error.category === "untrusted_base_url",
    );
  }
  assert.throws(
    () => validateExecutionBoundary(valid, { UAT_ENV: "production" }),
    (error) => error instanceof UatError && error.category === "staging_environment_required",
  );
  assert.throws(
    () => validateExecutionBoundary(valid, { UAT_ENV: "staging", NODE_TLS_REJECT_UNAUTHORIZED: "0" }),
    (error) => error instanceof UatError && error.category === "tls_verification_disabled",
  );
  assert.equal(validateExecutionBoundary(valid, { UAT_ENV: "staging" }).origin, STAGING_BASE_URL);
});

test("HTTP client never follows redirects", async (context) => {
  let destinationRequests = 0;
  const server = http.createServer((request, response) => {
    if (request.url === "/redirect") {
      response.writeHead(302, { location: "/destination" }).end();
      return;
    }
    destinationRequests += 1;
    response.writeHead(200).end("unexpected");
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  context.after(() => new Promise((resolve) => server.close(resolve)));
  const { port } = server.address();
  const request = createHttpClient({ baseUrl: `http://127.0.0.1:${port}` });
  await assert.rejects(
    request("/redirect"),
    (error) => error instanceof UatError && error.category === "redirect_rejected",
  );
  assert.equal(destinationRequests, 0);
});

test("receipt writer creates a redacted private file and refuses sensitive fields", () => {
  const directory = realpathSync(mkdtempSync(path.join(os.tmpdir(), "speakup-staging-uat-")));
  try {
    const privateDirectory = path.join(directory, "private");
    mkdirSync(privateDirectory, { mode: 0o700 });
    const receiptPath = path.join(privateDirectory, "receipt.json");
    const receipt = {
      receipt_version: 1,
      outcome: "passed",
      environment: "staging",
      resource_refs: ["sha256:" + "a".repeat(64)],
      sessions: [{ case_id: "part-1-text", error_category: null }],
    };
    writeReceipt(receiptPath, receipt);
    assert.deepEqual(JSON.parse(readFileSync(receiptPath, "utf8")), receipt);
    assert.equal(statSync(receiptPath).mode & 0o777, 0o600);
    assert.throws(
      () => writeReceipt(path.join(directory, "leak.json"), { session_token: "sess_secret" }),
      (error) => error instanceof UatError && error.category === "receipt_contains_sensitive_field",
    );
    assert.throws(
      () => writeReceipt(path.join(directory, "email.json"), { owner: "person@example.com" }),
      (error) => error instanceof UatError && error.category === "receipt_contains_sensitive_value",
    );
    const publicDirectory = path.join(directory, "public");
    mkdirSync(publicDirectory, { mode: 0o755 });
    chmodSync(publicDirectory, 0o755);
    assert.throws(
      () => writeReceipt(path.join(publicDirectory, "receipt.json"), receipt),
      (error) => error instanceof UatError && error.category === "receipt_directory_not_private",
    );
    const canonicalDirectory = path.join(directory, "canonical");
    mkdirSync(canonicalDirectory, { mode: 0o700 });
    const linkedDirectory = path.join(directory, "linked");
    symlinkSync(canonicalDirectory, linkedDirectory, "dir");
    assert.throws(
      () => writeReceipt(path.join(linkedDirectory, "receipt.json"), receipt),
      (error) => error instanceof UatError && error.category === "receipt_directory_not_private",
    );
    assert.throws(
      () => writeReceipt(path.join(directory, "missing", "receipt.json"), receipt),
      (error) => error instanceof UatError && error.category === "receipt_directory_required",
    );
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
});

test("an opted-in failed execution still writes only a redacted 0600 receipt", async () => {
  const directory = realpathSync(mkdtempSync(path.join(os.tmpdir(), "speakup-staging-uat-failed-")));
  try {
    const receiptPath = path.join(directory, "receipt.json");
    await assert.rejects(
      runCli({
        arguments_: ["--execute", "--receipt", receiptPath],
        environment: { UAT_ENV: "staging" },
        output: outputBuffer().output,
        executeUat: async () => { throw new UatError("evaluation_failed", "provider payload must not persist"); },
      }),
      (error) => error instanceof UatError && error.category === "evaluation_failed",
    );
    const receipt = JSON.parse(readFileSync(receiptPath, "utf8"));
    assert.equal(receipt.outcome, "failed");
    assert.equal(receipt.error_category, "evaluation_failed");
    assert.equal(statSync(receiptPath).mode & 0o777, 0o600);
    assert.doesNotMatch(JSON.stringify(receipt), /provider payload/);
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
});

test("five-session driver uses dynamic bank, plan, session, question, turn, and evaluation identities", async (context) => {
  const fake = await createUatServer();
  context.after(fake.close);
  const receipt = await runStagingUat({
    baseUrl: fake.baseUrl,
    voiceBytes: Buffer.from("RIFF fixed test WAV"),
    pollIntervalMs: 1,
  });
  assert.equal(receipt.outcome, "passed");
  assert.equal(fake.counts.plans, 5);
  assert.deepEqual(
    receipt.sessions.map(({ mode, input_kind }) => `${mode}:${input_kind}`),
    ["PART_1:text", "PART_1:voice", "PART_2:voice", "PART_3:voice", "FULL_MOCK:text"],
  );
  for (const session of receipt.sessions) {
    assert.equal(session.evaluation_status, "READY");
    assert.equal(session.schema_version, "evaluation-report/v2");
    assert.equal(session.scoreability_status, "PROVISIONAL");
    assert.match(session.resource_refs.plan, /^sha256:[0-9a-f]{64}$/);
    assert.match(session.resource_refs.session, /^sha256:[0-9a-f]{64}$/);
    assert.match(session.resource_refs.evaluation, /^sha256:[0-9a-f]{64}$/);
  }
  const serialized = JSON.stringify(receipt);
  assert.doesNotMatch(serialized, /sess_|example\.invalid|Voice fixture answer|Question 1/);
});

test("FAILED evaluation is a release-blocking terminal failure", async (context) => {
  const fake = await createUatServer("failed");
  context.after(fake.close);
  await assert.rejects(
    runStagingUat({
      baseUrl: fake.baseUrl,
      matrix: [UAT_MATRIX[0]],
      voiceBytes: Buffer.from("unused"),
    }),
    (error) => error instanceof UatError && error.category === "evaluation_failed",
  );
});

test("failed run receipt preserves its run identity and completed case progress", async (context) => {
  const fake = await createUatServer("fail-second");
  context.after(fake.close);
  const directory = realpathSync(mkdtempSync(path.join(os.tmpdir(), "speakup-staging-uat-progress-")));
  const receiptPath = path.join(directory, "receipt.json");
  try {
    let failure;
    await assert.rejects(
      runCli({
        arguments_: ["--execute", "--receipt", receiptPath],
        environment: { UAT_ENV: "staging" },
        output: outputBuffer().output,
        executeUat: () => runStagingUat({
          baseUrl: fake.baseUrl,
          matrix: [UAT_MATRIX[0], UAT_MATRIX[1]],
          voiceBytes: Buffer.from("RIFF fixed test WAV"),
          pollIntervalMs: 1,
        }),
      }),
      (error) => {
        failure = error;
        return error instanceof UatError && error.category === "evaluation_failed";
      },
    );
    const receipt = JSON.parse(readFileSync(receiptPath, "utf8"));
    assert.equal(receipt.run_id, failure.receipt.run_id);
    assert.equal(receipt.sessions.length, 2);
    assert.equal(receipt.sessions[0].evaluation_status, "READY");
    assert.equal(receipt.sessions[1].evaluation_status, "FAILED");
    assert.equal(receipt.sessions[1].error_category, "evaluation_failed");
    assert.match(receipt.sessions[1].resource_refs.plan, /^sha256:[0-9a-f]{64}$/);
    assert.match(receipt.sessions[1].resource_refs.session, /^sha256:[0-9a-f]{64}$/);
    assert.match(receipt.sessions[1].resource_refs.evaluation, /^sha256:[0-9a-f]{64}$/);
    assert.doesNotMatch(JSON.stringify(receipt), /sess_|example\.invalid|Voice fixture answer|Question 1/);
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
});

test("evaluation polling stops at the per-session deadline", async (context) => {
  const fake = await createUatServer("timeout");
  context.after(fake.close);
  let clock = 0;
  await assert.rejects(
    runStagingUat({
      baseUrl: fake.baseUrl,
      matrix: [{ ...UAT_MATRIX[0], timeout_seconds: 1 }],
      voiceBytes: Buffer.from("unused"),
      now: () => clock,
      pollIntervalMs: 600,
      sleep: async (milliseconds) => { clock += milliseconds; },
    }),
    (error) => error instanceof UatError && error.category === "session_timeout",
  );
});

for (const [name, behavior, category] of [
  ["voice candidate identity", "bad-candidate-binding", "transcription_candidate_mismatch"],
  ["confirmed voice turn identity", "bad-turn-binding", "turn_confirmation_mismatch"],
]) {
  test(`${name} is bound end to end`, async (context) => {
    const fake = await createUatServer(behavior);
    context.after(fake.close);
    await assert.rejects(
      runStagingUat({
        baseUrl: fake.baseUrl,
        matrix: [UAT_MATRIX[1]],
        voiceBytes: Buffer.from("RIFF fixed test WAV"),
      }),
      (error) => error instanceof UatError && error.category === category,
    );
  });
}

test("failure before evaluation is reported as not reached", async (context) => {
  const fake = await createUatServer("bad-candidate-binding");
  context.after(fake.close);
  let failure;
  await assert.rejects(
    runStagingUat({
      baseUrl: fake.baseUrl,
      matrix: [UAT_MATRIX[1]],
      voiceBytes: Buffer.from("RIFF fixed test WAV"),
    }),
    (error) => {
      failure = error;
      return error instanceof UatError && error.category === "transcription_candidate_mismatch";
    },
  );
  assert.equal(failure.receipt.sessions[0].evaluation_status, "NOT_REACHED");
  assert.equal(Object.hasOwn(failure.receipt.sessions[0].resource_refs, "evaluation"), false);
});

test("report evidence must resolve the exact UTF-8 byte range", async (context) => {
  const fake = await createUatServer("invalid-evidence");
  context.after(fake.close);
  await assert.rejects(
    runStagingUat({
      baseUrl: fake.baseUrl,
      matrix: [UAT_MATRIX[0]],
      voiceBytes: Buffer.from("unused"),
    }),
    (error) => error instanceof UatError && error.category === "invalid_report_evidence",
  );
});

test("INSUFFICIENT report cannot expose a score", async (context) => {
  const fake = await createUatServer("insufficient-score");
  context.after(fake.close);
  await assert.rejects(
    runStagingUat({
      baseUrl: fake.baseUrl,
      matrix: [UAT_MATRIX[0]],
      voiceBytes: Buffer.from("unused"),
    }),
    (error) => error instanceof UatError && error.category === "insufficient_report_has_score",
  );
});

for (const [name, behavior, category] of [
  ["health JSON status", "bad-health", "staging_not_ready"],
  ["readiness database check", "bad-readiness", "staging_not_ready"],
  ["login no-store response", "bad-login-cache", "invalid_auth_cache_control"],
  ["question bank schema version", "bad-bank-version", "invalid_question_bank"],
  ["plan assignment bank binding", "bad-assignment-bank", "plan_mode_mismatch"],
  ["plan assignment part order", "bad-assignment-parts", "plan_mode_mismatch"],
  ["session frozen assignment", "bad-snapshot-freeze", "session_binding_mismatch"],
  ["interaction plan identity", "bad-state-plan", "interaction_state_mismatch"],
  ["interaction frozen assignment", "bad-state-assignment", "interaction_state_mismatch"],
  ["interaction lifecycle", "bad-lifecycle", "invalid_interaction_lifecycle"],
  ["nonterminal evaluation payload exclusion", "queued-with-result", "invalid_evaluation_state"],
  ["IELTS dimension score step", "invalid-dimension", "invalid_report_dimension"],
]) {
  test(`${name} is validated fail closed`, async (context) => {
    const fake = await createUatServer(behavior);
    context.after(fake.close);
    await assert.rejects(
      runStagingUat({
        baseUrl: fake.baseUrl,
        matrix: [UAT_MATRIX[0]],
        voiceBytes: Buffer.from("unused"),
      }),
      (error) => error instanceof UatError && error.category === category,
    );
  });
}
