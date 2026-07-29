import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";

function unique(values) {
  return [...new Set(values)];
}

function sameSet(left, right) {
  const a = [...left].sort();
  const b = [...right].sort();
  return a.length === b.length && a.every((value, index) => value === b[index]);
}

function eventsForRun(events, runId) {
  return events.filter((event) => event.run_id === runId);
}

export function parseJsonLog(content) {
  const events = [];
  for (const line of content.split(/\r?\n/)) {
    if (!line.trim()) continue;
    try {
      events.push(JSON.parse(line));
    } catch {
      // Build output may precede structured server logs.
    }
  }
  return events;
}

export function evaluateCases(cases, executions, events) {
  const executionByName = new Map(
    executions.map((execution) => [execution.name, execution]),
  );

  return cases.map((testCase) => {
    const execution = executionByName.get(testCase.name);
    const runId = execution?.target_run_id;
    const runEvents = runId ? eventsForRun(events, runId) : [];
    const decisions = runEvents.filter(
      (event) => event.msg === "agent.routing.decision",
    );
    const started = runEvents.filter(
      (event) => event.msg === "agent.tool.call.started",
    );
    const succeeded = runEvents.filter(
      (event) => event.msg === "agent.tool.call.succeeded",
    );
    const failed = runEvents.filter(
      (event) => event.msg === "agent.tool.call.failed",
    );
    const completed = runEvents.find(
      (event) => event.msg === "agent.run.completed",
    );

    const actualDecision = decisions[0]?.decision ?? "";
    const startedTools = started.map((event) => event.tool_name);
    const succeededCallIds = new Set(
      succeeded.map((event) => event.tool_call_id),
    );
    const expectedTools = testCase.expected_tools ?? [];
    const forbiddenTools = testCase.forbidden_tools ?? [];
    const duplicateTools = unique(startedTools).filter(
      (tool) => startedTools.filter((value) => value === tool).length > 1,
    );

    const transportPassed =
      execution?.http_ok === true &&
      execution?.status === "completed" &&
      Boolean(completed);
    const decisionPassed =
      actualDecision === testCase.expected_decision;
    const toolSelectionPassed = sameSet(unique(startedTools), expectedTools);
    const forbiddenPassed = forbiddenTools.every(
      (tool) => !startedTools.includes(tool),
    );
    const executionPassed =
      failed.length === 0 &&
      started.every((event) => succeededCallIds.has(event.tool_call_id));
    const duplicatePassed = duplicateTools.length === 0;
    const passed =
      transportPassed &&
      decisionPassed &&
      toolSelectionPassed &&
      forbiddenPassed &&
      executionPassed &&
      duplicatePassed;

    const reasons = [];
    if (!transportPassed) reasons.push("HTTP 或 Run 未完成");
    if (!decisionPassed) reasons.push("决策不匹配");
    if (!toolSelectionPassed) reasons.push("工具集合不匹配");
    if (!forbiddenPassed) reasons.push("调用了禁用工具");
    if (!executionPassed) reasons.push("工具执行未全部成功");
    if (!duplicatePassed) reasons.push("存在重复工具调用");

    return {
      name: testCase.name,
      message: testCase.messages.at(-1),
      run_id: runId ?? "",
      thread_id: execution?.thread_id ?? "",
      expected_decision: testCase.expected_decision,
      actual_decision: actualDecision || "(missing)",
      expected_tools: expectedTools,
      actual_tools: startedTools,
      forbidden_tools: forbiddenTools,
      duplicate_tools: duplicateTools,
      transport_passed: transportPassed,
      decision_passed: decisionPassed,
      tool_selection_passed: toolSelectionPassed,
      forbidden_passed: forbiddenPassed,
      execution_passed: executionPassed,
      duplicate_passed: duplicatePassed,
      passed,
      reason: reasons.join("；") || "符合预期",
      provider: execution?.provider ?? "",
      model: execution?.model ?? "",
    };
  });
}

function ratio(results, predicate, denominator = results.length) {
  const passed = results.filter(predicate).length;
  return {
    passed,
    total: denominator,
    percentage: denominator === 0 ? 0 : (passed / denominator) * 100,
  };
}

export function calculateMetrics(results) {
  const expectedToolCases = results.filter(
    (result) => result.expected_tools.length > 0,
  );
  const guardedCases = results.filter(
    (result) => result.forbidden_tools.length > 0,
  );
  const duplicateCount = results.filter(
    (result) => result.duplicate_tools.length > 0,
  ).length;

  return {
    overall: ratio(results, (result) => result.passed),
    decision: ratio(results, (result) => result.decision_passed),
    tool_selection: ratio(
      expectedToolCases,
      (result) => result.tool_selection_passed,
    ),
    forbidden_safety: ratio(
      guardedCases,
      (result) => result.forbidden_passed,
    ),
    tool_execution: ratio(results, (result) => result.execution_passed),
    duplicate_rate: {
      passed: duplicateCount,
      total: results.length,
      percentage:
        results.length === 0 ? 0 : (duplicateCount / results.length) * 100,
    },
  };
}

function percent(metric) {
  return `${metric.percentage.toFixed(1)}% (${metric.passed}/${metric.total})`;
}

function markdownCell(value) {
  return String(value ?? "")
    .replaceAll("|", "\\|")
    .replaceAll("\n", "<br>");
}

function toolList(tools) {
  return tools.length === 0 ? "-" : tools.join("<br>");
}

export function renderMarkdown(metadata, results, metrics) {
  const rows = results
    .map(
      (result) =>
        `| ${result.passed ? "PASS" : "FAIL"} | ${markdownCell(result.name)} | ${markdownCell(result.message)} | ${markdownCell(result.expected_decision)} | ${markdownCell(result.actual_decision)} | ${toolList(result.expected_tools)} | ${toolList(result.actual_tools)} | ${markdownCell(result.reason)} |`,
    )
    .join("\n");

  return `# Agent Routing Benchmark

- 时间：${metadata.generated_at}
- Git：${metadata.git_revision || "(unknown)"}
- 服务地址：${metadata.base_url}
- Provider / Model：${metadata.provider || "(unknown)"} / ${metadata.model || "(unknown)"}
- 用例文件：${metadata.cases_file}

## 指标

| 指标 | 结果 |
| --- | ---: |
| 总体准确率 | ${percent(metrics.overall)} |
| 决策准确率 | ${percent(metrics.decision)} |
| 工具选择准确率 | ${percent(metrics.tool_selection)} |
| 禁用工具安全率 | ${percent(metrics.forbidden_safety)} |
| 工具执行成功率 | ${percent(metrics.tool_execution)} |
| 重复工具调用率 | ${percent(metrics.duplicate_rate)} |

## 明细

| 结果 | Case | 用户消息（目标 turn） | 期望决策 | 实际决策 | 期望工具 | 实际工具 | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- |
${rows}

## 审计关联

每条明细的原始 JSON 中保留 \`thread_id\` 和 \`run_id\`，可与服务日志中的 \`agent.routing.decision\`、\`agent.tool.call.started\`、\`agent.tool.call.succeeded\` 关联。
`;
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

export function renderHtml(metadata, results, metrics) {
  const metricCards = [
    ["总体准确率", metrics.overall],
    ["决策准确率", metrics.decision],
    ["工具选择准确率", metrics.tool_selection],
    ["禁用工具安全率", metrics.forbidden_safety],
    ["工具执行成功率", metrics.tool_execution],
    ["重复调用率", metrics.duplicate_rate],
  ]
    .map(
      ([label, metric]) => `<div class="metric">
        <span>${escapeHtml(label)}</span>
        <strong>${escapeHtml(percent(metric))}</strong>
      </div>`,
    )
    .join("");
  const rows = results
    .map(
      (result) => `<tr>
        <td><span class="status ${result.passed ? "pass" : "fail"}">${result.passed ? "PASS" : "FAIL"}</span></td>
        <td><code>${escapeHtml(result.name)}</code></td>
        <td>${escapeHtml(result.message)}</td>
        <td>${escapeHtml(result.expected_decision)}</td>
        <td>${escapeHtml(result.actual_decision)}</td>
        <td>${escapeHtml(result.expected_tools.join(", ") || "-")}</td>
        <td>${escapeHtml(result.actual_tools.join(", ") || "-")}</td>
        <td>${escapeHtml(result.reason)}</td>
      </tr>`,
    )
    .join("");

  return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Agent Routing Benchmark</title>
  <style>
    :root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; color: #17201d; background: #f4f6f5; }
    header { padding: 32px max(24px, 4vw) 24px; color: #fff; background: #173f35; }
    h1 { margin: 0 0 12px; font-size: 28px; letter-spacing: 0; }
    .meta { display: flex; flex-wrap: wrap; gap: 8px 20px; color: #d7e6e1; font-size: 13px; }
    main { padding: 24px max(24px, 4vw) 48px; }
    .metrics { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; margin-bottom: 24px; }
    .metric { padding: 15px; border: 1px solid #dbe2df; border-radius: 6px; background: #fff; }
    .metric span { display: block; color: #5c6864; font-size: 13px; }
    .metric strong { display: block; margin-top: 6px; font-size: 20px; }
    .table-wrap { overflow-x: auto; border: 1px solid #dbe2df; background: #fff; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th, td { padding: 11px 12px; border-bottom: 1px solid #e7ecea; text-align: left; vertical-align: top; }
    th { position: sticky; top: 0; color: #43504c; background: #eef2f0; white-space: nowrap; }
    code { color: #184c7a; }
    .status { display: inline-block; min-width: 42px; padding: 3px 6px; border-radius: 4px; color: #fff; font-weight: 700; text-align: center; }
    .pass { background: #26734d; }
    .fail { background: #b63838; }
    footer { margin-top: 16px; color: #66736f; font-size: 12px; }
  </style>
</head>
<body>
  <header>
    <h1>Agent Routing Benchmark</h1>
    <div class="meta">
      <span>${escapeHtml(metadata.generated_at)}</span>
      <span>Git ${escapeHtml(metadata.git_revision || "(unknown)")}</span>
      <span>${escapeHtml(metadata.provider || "(unknown)")} / ${escapeHtml(metadata.model || "(unknown)")}</span>
    </div>
  </header>
  <main>
    <section class="metrics">${metricCards}</section>
    <div class="table-wrap">
      <table>
        <thead><tr><th>结果</th><th>Case</th><th>用户消息</th><th>期望决策</th><th>实际决策</th><th>期望工具</th><th>实际工具</th><th>说明</th></tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
    <footer>原始 JSON 保留 thread_id / run_id，用于关联结构化服务日志。</footer>
  </main>
</body>
</html>`;
}

export async function writeReports({
  reportDirectory,
  reportName,
  metadata,
  results,
  metrics,
  executions,
  logContent,
}) {
  const markdown = renderMarkdown(metadata, results, metrics);
  const html = renderHtml(metadata, results, metrics);
  const raw = JSON.stringify({ metadata, metrics, results, executions }, null, 2);
  const outputs = {
    markdown: path.join(reportDirectory, `${reportName}.md`),
    html: path.join(reportDirectory, `${reportName}.html`),
    json: path.join(reportDirectory, `${reportName}.json`),
    log: path.join(reportDirectory, `${reportName}.server.log`),
  };

  await Promise.all([
    writeFile(outputs.markdown, markdown),
    writeFile(outputs.html, html),
    writeFile(outputs.json, raw),
    writeFile(outputs.log, logContent),
    writeFile(path.join(reportDirectory, "latest.md"), markdown),
    writeFile(path.join(reportDirectory, "latest.html"), html),
    writeFile(path.join(reportDirectory, "latest.json"), raw),
  ]);
  return outputs;
}

export async function loadAndEvaluate(casesFile, executionFile, logFile) {
  const [casesRaw, executionsRaw, logContent] = await Promise.all([
    readFile(casesFile, "utf8"),
    readFile(executionFile, "utf8"),
    readFile(logFile, "utf8"),
  ]);
  const cases = JSON.parse(casesRaw);
  const executions = JSON.parse(executionsRaw);
  const events = parseJsonLog(logContent);
  const results = evaluateCases(cases, executions, events);
  return { cases, executions, logContent, results };
}
