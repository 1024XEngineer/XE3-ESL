import assert from "node:assert/strict";
import test from "node:test";
import {
  calculateMetrics,
  evaluateCases,
  parseJsonLog,
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
