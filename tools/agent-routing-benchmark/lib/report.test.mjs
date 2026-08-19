import assert from "node:assert/strict";
import test from "node:test";
import {
  calculateMetrics,
  evaluateCases,
  parseJsonLog,
  renderHtml,
  renderMarkdown,
} from "./report.mjs";

test("evaluates routing, successful tool execution, and duplicates by run id", () => {
  const cases = [
    {
      name: "tool_case",
      messages: ["查看评价"],
      expected_decision: "tool_call",
      expected_tools: ["review.search.v1"],
      forbidden_tools: ["scenario.create.v1"],
    },
    {
      name: "direct_case",
      messages: ["润色"],
      expected_decision: "direct_response",
      expected_tools: [],
    },
  ];
  const executions = [
    {
      name: "tool_case",
      target_run_id: "run-1",
      thread_id: "thread-1",
      http_ok: true,
      status: "completed",
    },
    {
      name: "direct_case",
      target_run_id: "run-2",
      thread_id: "thread-2",
      http_ok: true,
      status: "completed",
    },
  ];
  const log = [
    { msg: "agent.routing.decision", run_id: "run-1", decision: "tool_call" },
    {
      msg: "agent.tool.call.started",
      run_id: "run-1",
      tool_call_id: "call-1",
      tool_name: "review.search.v1",
    },
    {
      msg: "agent.tool.call.succeeded",
      run_id: "run-1",
      tool_call_id: "call-1",
      tool_name: "review.search.v1",
    },
    { msg: "agent.run.completed", run_id: "run-1" },
    {
      msg: "agent.routing.decision",
      run_id: "unrelated",
      decision: "tool_call",
    },
    {
      msg: "agent.routing.decision",
      run_id: "run-2",
      decision: "direct_response",
    },
    { msg: "agent.run.completed", run_id: "run-2" },
  ]
    .map(JSON.stringify)
    .join("\n");

  const results = evaluateCases(cases, executions, parseJsonLog(log));
  assert.equal(results[0].passed, true);
  assert.equal(results[1].passed, true);
  assert.deepEqual(results[0].actual_tools, ["review.search.v1"]);
  assert.deepEqual(calculateMetrics(results).overall, {
    passed: 2,
    total: 2,
    percentage: 100,
  });
});

test("fails forbidden and duplicate tool calls and renders the reason", () => {
  const cases = [
    {
      name: "guarded",
      messages: ["先读后写"],
      expected_decision: "tool_call",
      expected_tools: ["review.search.v1"],
      forbidden_tools: ["scenario.create.v1"],
    },
  ];
  const executions = [
    {
      name: "guarded",
      target_run_id: "run-1",
      http_ok: true,
      status: "completed",
    },
  ];
  const events = [
    { msg: "agent.routing.decision", run_id: "run-1", decision: "tool_call" },
    ...["call-1", "call-2"].flatMap((callId) => [
      {
        msg: "agent.tool.call.started",
        run_id: "run-1",
        tool_call_id: callId,
        tool_name: "scenario.create.v1",
      },
      {
        msg: "agent.tool.call.succeeded",
        run_id: "run-1",
        tool_call_id: callId,
        tool_name: "scenario.create.v1",
      },
    ]),
    { msg: "agent.run.completed", run_id: "run-1" },
  ];

  const results = evaluateCases(cases, executions, events);
  assert.equal(results[0].passed, false);
  assert.equal(results[0].forbidden_passed, false);
  assert.deepEqual(results[0].duplicate_tools, ["scenario.create.v1"]);
  assert.match(
    renderMarkdown(
      {
        generated_at: "now",
        base_url: "local",
        cases_file: "cases.json",
      },
      results,
      calculateMetrics(results),
    ),
    /存在重复工具调用/,
  );
});

test("evaluates required, forbidden, paragraph, and sentence response rules", () => {
  const cases = [
    {
      name: "natural_warmup",
      messages: ["创建雅思 Part 2 人物类专项练习"],
      expected_decision: "tool_call",
      expected_tools: ["ielts.warmup.v1"],
      required_response_terms: ["最近有没有谁", "一两句英语"],
      forbidden_response_terms: [
        "Warm-up",
        "PART_2",
        "\n- ",
        "不用管对错",
        "卡住",
        "提示",
        "直接开始",
        "I'd like to talk about ...",
      ],
      max_non_empty_paragraphs: 1,
    },
    {
      name: "leaky_preview",
      messages: ["直接开始"],
      expected_decision: "tool_call",
      expected_tools: ["practice.preview.v3"],
      required_response_terms: ["好。"],
      forbidden_response_terms: ["PART_1", "5 分钟", "准备好", "卡片"],
      max_non_empty_paragraphs: 1,
      max_sentences: 1,
    },
  ];
  const executions = [
    {
      name: "natural_warmup",
      target_run_id: "run-warmup",
      thread_id: "thread-warmup",
      assistant_message_id: "message-warmup",
      assistant_response:
        "可以。最近有没有谁让你印象挺深？用一两句英语说说。",
      http_ok: true,
      status: "completed",
    },
    {
      name: "leaky_preview",
      target_run_id: "run-preview",
      thread_id: "thread-preview",
      assistant_message_id: "message-preview",
      assistant_response:
        "PART_1 已准备好。\n\n预计 5 分钟。请点击卡片开始。",
      http_ok: true,
      status: "completed",
    },
  ];
  const events = [
    ...successfulToolEvents("run-warmup", "ielts.warmup.v1"),
    ...successfulToolEvents("run-preview", "practice.preview.v3"),
  ];

  const results = evaluateCases(cases, executions, events);
  assert.equal(results[0].passed, true);
  assert.equal(results[0].response_contract_passed, true);
  assert.equal(results[0].response_paragraph_count, 1);
  assert.equal(results[1].passed, false);
  assert.equal(results[1].response_contract_passed, false);
  assert.deepEqual(results[1].present_forbidden_response_terms, [
    "PART_1",
    "5 分钟",
    "准备好",
    "卡片",
  ]);
  assert.equal(results[1].response_paragraph_count, 2);
  assert.equal(results[1].response_sentence_count, 3);
  assert.match(results[1].reason, /回复包含禁用内容/);
  assert.match(results[1].reason, /回复段落数 2 > 1/);
  assert.match(results[1].reason, /回复句数 3 > 1/);
  assert.deepEqual(calculateMetrics(results).response_contract, {
    passed: 1,
    total: 2,
    percentage: 50,
  });
});

test("evaluates the structured preview resolution without raw scene text", () => {
  const cases = [
    {
      name: "catalog_preview",
      messages: ["自然语言场景请求"],
      expected_decision: "tool_call",
      expected_tools: ["practice.preview.v3"],
      expected_preview_input: {
        kind: "CATALOG",
        catalog_scene_id: "scn_travel_hotel_checkin",
      },
    },
    {
      name: "wrong_custom_preview",
      messages: ["另一个自然语言场景请求"],
      expected_decision: "tool_call",
      expected_tools: ["practice.preview.v3"],
      expected_preview_input: {
        kind: "CUSTOM",
      },
    },
    {
      name: "clarification_subset",
      messages: ["大类请求"],
      expected_decision: "tool_call",
      expected_tools: ["practice.preview.v3"],
      expected_preview_input: {
        kind: "NEEDS_CLARIFICATION",
        candidate_scene_ids: ["scene_a", "scene_b", "scene_c"],
        candidate_scene_ids_mode: "subset",
      },
    },
  ];
  const executions = cases.map((testCase, index) => ({
    name: testCase.name,
    target_run_id: `run-${index + 1}`,
    http_ok: true,
    status: "completed",
  }));
  executions.push({
    name: "clarification_subset",
    target_run_id: "run-3",
    http_ok: true,
    status: "completed",
  });
  const events = [
    ...successfulToolEvents("run-1", "practice.preview.v3"),
    {
      msg: "agent.benchmark.preview.input",
      run_id: "run-1",
      kind: "CATALOG",
      catalog_scene_id: "scn_travel_hotel_checkin",
    },
    ...successfulToolEvents("run-2", "practice.preview.v3"),
    {
      msg: "agent.benchmark.preview.input",
      run_id: "run-2",
      kind: "CATALOG",
      catalog_scene_id: "scn_daily_small_talk",
    },
    ...successfulToolEvents("run-3", "practice.preview.v3"),
    {
      msg: "agent.benchmark.preview.input",
      run_id: "run-3",
      kind: "NEEDS_CLARIFICATION",
      catalog_scene_id: "",
      candidate_scene_ids: ["scene_b", "scene_c"],
    },
  ];

  const results = evaluateCases(cases, executions, events);
  assert.equal(results[0].passed, true);
  assert.deepEqual(results[0].actual_preview_input, {
    kind: "CATALOG",
    catalog_scene_id: "scn_travel_hotel_checkin",
    candidate_scene_ids: [],
  });
  assert.equal(results[1].passed, false);
  assert.equal(results[1].preview_input_contract_passed, false);
  assert.match(results[1].reason, /Preview 场景决议输入不匹配/);
  assert.equal(results[2].passed, true);
  assert.equal(results[2].preview_input_contract_passed, true);
  assert.deepEqual(calculateMetrics(results).preview_input_contract, {
    passed: 2,
    total: 3,
    percentage: (2 / 3) * 100,
  });
});

test("does not persist raw user messages in benchmark results or reports", () => {
  const privateMessage = "PRIVATE_ORIGINAL_QUERY";
  const cases = [
    {
      name: "private_preview",
      messages: [privateMessage],
      expected_decision: "direct_response",
      expected_tools: [],
    },
  ];
  const executions = [
    {
      name: "private_preview",
      target_run_id: "run-private",
      thread_id: "thread-private",
      assistant_message_id: "message-private",
      assistant_response: "已处理。",
      http_ok: true,
      status: "completed",
    },
  ];
  const events = [
    {
      msg: "agent.routing.decision",
      run_id: "run-private",
      decision: "direct_response",
    },
    { msg: "agent.run.completed", run_id: "run-private" },
  ];
  const metadata = {
    generated_at: "now",
    base_url: "local",
    cases_file: "cases.json",
  };

  const results = evaluateCases(cases, executions, events);
  const metrics = calculateMetrics(results);

  assert.doesNotMatch(JSON.stringify(results), new RegExp(privateMessage));
  assert.doesNotMatch(
    renderMarkdown(metadata, results, metrics),
    new RegExp(privateMessage),
  );
  assert.doesNotMatch(
    renderHtml(metadata, results, metrics),
    new RegExp(privateMessage),
  );
});

function successfulToolEvents(runId, toolName) {
  const callId = `${runId}-call`;
  return [
    { msg: "agent.routing.decision", run_id: runId, decision: "tool_call" },
    {
      msg: "agent.tool.call.started",
      run_id: runId,
      tool_call_id: callId,
      tool_name: toolName,
    },
    {
      msg: "agent.tool.call.succeeded",
      run_id: runId,
      tool_call_id: callId,
      tool_name: toolName,
    },
    { msg: "agent.run.completed", run_id: runId },
  ];
}
