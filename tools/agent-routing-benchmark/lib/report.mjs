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

function nonEmptyParagraphCount(content) {
  const trimmed = String(content ?? "").trim();
  if (!trimmed) return 0;
  return trimmed.split(/\r?\n\s*\r?\n+/).filter((value) => value.trim()).length;
}

function sentenceCount(content) {
  const normalized = String(content ?? "").replace(/\.{2,}/g, "").trim();
  if (!normalized) return 0;
  return normalized
    .split(/[。！？!?]+|[.](?=\s|$)/u)
    .filter((value) => value.trim()).length;
}

function responseContract(testCase, response) {
  const requiredTerms = testCase.required_response_terms ?? [];
  const forbiddenTerms = testCase.forbidden_response_terms ?? [];
  const maxParagraphs = testCase.max_non_empty_paragraphs;
  const maxSentences = testCase.max_sentences;
  const checked =
    requiredTerms.length > 0 ||
    forbiddenTerms.length > 0 ||
    Number.isInteger(maxParagraphs) ||
    Number.isInteger(maxSentences);
  const normalized = String(response ?? "").toLocaleLowerCase();
  const missingTerms = requiredTerms.filter(
    (term) => !normalized.includes(String(term).toLocaleLowerCase()),
  );
  const presentForbiddenTerms = forbiddenTerms.filter(
    (term) => normalized.includes(String(term).toLocaleLowerCase()),
  );
  const paragraphCount = nonEmptyParagraphCount(response);
  const actualSentenceCount = sentenceCount(response);
  const paragraphsPassed =
    !Number.isInteger(maxParagraphs) || paragraphCount <= maxParagraphs;
  const sentencesPassed =
    !Number.isInteger(maxSentences) || actualSentenceCount <= maxSentences;
  return {
    checked,
    passed:
      (!checked || String(response ?? "").trim().length > 0) &&
      missingTerms.length === 0 &&
      presentForbiddenTerms.length === 0 &&
      paragraphsPassed &&
      sentencesPassed,
    requiredTerms,
    forbiddenTerms,
    missingTerms,
    presentForbiddenTerms,
    paragraphCount,
    sentenceCount: actualSentenceCount,
    maxParagraphs,
    maxSentences,
    paragraphsPassed,
    sentencesPassed,
  };
}

function previewInputContract(testCase, runEvents) {
  const expected = testCase.expected_preview_input;
  if (!expected) {
    return { checked: false, passed: true, expected: null, actual: null };
  }
  const inputs = runEvents.filter(
    (event) => event.msg === "agent.benchmark.preview.input",
  );
  if (inputs.length !== 1) {
    return {
      checked: true,
      passed: false,
      expected,
      actual: null,
      eventCount: inputs.length,
    };
  }
  const event = inputs[0];
  const actual = {
    kind: event.kind ?? "",
    catalog_scene_id: event.catalog_scene_id ?? "",
    candidate_scene_ids: Array.isArray(event.candidate_scene_ids)
      ? event.candidate_scene_ids
      : [],
    ielts_practice_mode: event.ielts_practice_mode ?? "",
    ielts_topic_choice: event.ielts_topic_choice ?? "",
  };
  const expectedCandidates = expected.candidate_scene_ids ?? [];
  const candidateMode = expected.candidate_scene_ids_mode ?? "exact";
  const candidateIDsPassed =
    candidateMode === "subset"
      ? actual.candidate_scene_ids.length > 0 &&
        actual.candidate_scene_ids.every((id) => expectedCandidates.includes(id))
      : sameSet(actual.candidate_scene_ids, expectedCandidates);
  return {
    checked: true,
    passed:
      actual.kind === expected.kind &&
      actual.catalog_scene_id === (expected.catalog_scene_id ?? "") &&
      candidateIDsPassed &&
      actual.ielts_practice_mode === (expected.ielts_practice_mode ?? "") &&
      actual.ielts_topic_choice === (expected.ielts_topic_choice ?? ""),
    expected: {
      kind: expected.kind,
      catalog_scene_id: expected.catalog_scene_id ?? "",
      candidate_scene_ids: expectedCandidates,
      candidate_scene_ids_mode: candidateMode,
      ielts_practice_mode: expected.ielts_practice_mode ?? "",
      ielts_topic_choice: expected.ielts_topic_choice ?? "",
    },
    actual,
    eventCount: 1,
  };
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
    const assistantResponse = execution?.assistant_response ?? "";
    const response = responseContract(testCase, assistantResponse);
    const previewInput = previewInputContract(testCase, runEvents);
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
      duplicatePassed &&
      previewInput.passed &&
      response.passed;

    const reasons = [];
    if (!transportPassed) reasons.push("HTTP 或 Run 未完成");
    if (!decisionPassed) reasons.push("决策不匹配");
    if (!toolSelectionPassed) reasons.push("工具集合不匹配");
    if (!forbiddenPassed) reasons.push("调用了禁用工具");
    if (!executionPassed) reasons.push("工具执行未全部成功");
    if (!duplicatePassed) reasons.push("存在重复工具调用");
    if (!previewInput.passed) {
      if (previewInput.eventCount !== 1) {
        reasons.push(
          `Preview 结构化输入记录数 ${previewInput.eventCount}，期望 1`,
        );
      } else {
        reasons.push("Preview 场景决议输入不匹配");
      }
    }
    if (response.checked && !assistantResponse.trim()) {
      reasons.push("缺少目标 Assistant 回复");
    }
    if (response.missingTerms.length > 0) {
      reasons.push(`回复缺少：${response.missingTerms.join("、")}`);
    }
    if (response.presentForbiddenTerms.length > 0) {
      reasons.push(
        `回复包含禁用内容：${response.presentForbiddenTerms.join("、")}`,
      );
    }
    if (!response.paragraphsPassed) {
      reasons.push(
        `回复段落数 ${response.paragraphCount} > ${response.maxParagraphs}`,
      );
    }
    if (!response.sentencesPassed) {
      reasons.push(
        `回复句数 ${response.sentenceCount} > ${response.maxSentences}`,
      );
    }

    return {
      name: testCase.name,
      run_id: runId ?? "",
      thread_id: execution?.thread_id ?? "",
      assistant_message_id: execution?.assistant_message_id ?? "",
      assistant_response: assistantResponse,
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
      preview_input_contract_checked: previewInput.checked,
      preview_input_contract_passed: previewInput.passed,
      expected_preview_input: previewInput.expected,
      actual_preview_input: previewInput.actual,
      response_contract_checked: response.checked,
      response_contract_passed: response.passed,
      required_response_terms: response.requiredTerms,
      forbidden_response_terms: response.forbiddenTerms,
      missing_response_terms: response.missingTerms,
      present_forbidden_response_terms: response.presentForbiddenTerms,
      response_paragraph_count: response.paragraphCount,
      response_sentence_count: response.sentenceCount,
      max_non_empty_paragraphs: response.maxParagraphs,
      max_sentences: response.maxSentences,
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
  const responseCases = results.filter(
    (result) => result.response_contract_checked,
  );
  const previewInputCases = results.filter(
    (result) => result.preview_input_contract_checked,
  );

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
    response_contract: ratio(
      responseCases,
      (result) => result.response_contract_passed,
    ),
    preview_input_contract: ratio(
      previewInputCases,
      (result) => result.preview_input_contract_passed,
    ),
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
        `| ${result.passed ? "PASS" : "FAIL"} | ${markdownCell(result.name)} | ${markdownCell(result.assistant_response)} | ${markdownCell(result.expected_decision)} | ${markdownCell(result.actual_decision)} | ${toolList(result.expected_tools)} | ${toolList(result.actual_tools)} | ${markdownCell(result.reason)} |`,
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
| Preview 输入契约通过率 | ${percent(metrics.preview_input_contract)} |
| 回复契约通过率 | ${percent(metrics.response_contract)} |
| 重复工具调用率 | ${percent(metrics.duplicate_rate)} |

## 明细

| 结果 | Case | Assistant 回复 | 期望决策 | 实际决策 | 期望工具 | 实际工具 | 说明 |
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
  const pageData = JSON.stringify({
    reportId: metadata.report_id,
    suiteFingerprint: metadata.suite_fingerprint,
    overallAccuracy: metrics.overall.percentage,
  }).replaceAll("<", "\\u003c");
  const metricCards = [
    ["总体准确率", metrics.overall],
    ["决策准确率", metrics.decision],
    ["工具选择准确率", metrics.tool_selection],
    ["禁用工具安全率", metrics.forbidden_safety],
    ["工具执行成功率", metrics.tool_execution],
    ["Preview 输入契约通过率", metrics.preview_input_contract],
    ["回复契约通过率", metrics.response_contract],
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
        <td>${escapeHtml(result.assistant_response)}</td>
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
    .history { margin-bottom: 24px; padding: 18px; border: 1px solid #dbe2df; background: #fff; }
    .history-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
    .history h2 { margin: 0; font-size: 17px; letter-spacing: 0; }
    .record-controls { display: flex; align-items: center; gap: 8px; }
    .record-controls input { width: min(260px, 28vw); min-height: 36px; box-sizing: border-box; padding: 0 10px; border: 1px solid #cbd5d1; border-radius: 5px; color: #17201d; background: #fff; font: inherit; }
    .record-controls input:focus { border-color: #176b52; outline: 2px solid #cce2da; outline-offset: 0; }
    .record-controls input:disabled { color: #68736f; background: #eef2f0; }
    .history button { min-height: 36px; padding: 0 13px; border: 0; border-radius: 5px; color: #fff; background: #176b52; font: inherit; font-weight: 650; cursor: pointer; }
    .history button:hover { background: #115742; }
    .history button:disabled { color: #68736f; background: #e2e7e5; cursor: default; }
    .comparison { min-height: 20px; margin: 0 0 12px; color: #43504c; font-size: 14px; }
    .chart-wrap { position: relative; width: 100%; height: 260px; }
    canvas { display: block; width: 100%; height: 100%; }
    .chart-empty { position: absolute; inset: 0; display: grid; place-items: center; color: #75817d; font-size: 13px; pointer-events: none; }
    .table-wrap { overflow-x: auto; border: 1px solid #dbe2df; background: #fff; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th, td { padding: 11px 12px; border-bottom: 1px solid #e7ecea; text-align: left; vertical-align: top; }
    th { position: sticky; top: 0; color: #43504c; background: #eef2f0; white-space: nowrap; }
    code { color: #184c7a; }
    .status { display: inline-block; min-width: 42px; padding: 3px 6px; border-radius: 4px; color: #fff; font-weight: 700; text-align: center; }
    .pass { background: #26734d; }
    .fail { background: #b63838; }
    footer { margin-top: 16px; color: #66736f; font-size: 12px; }
    @media (max-width: 640px) {
      .history-head { align-items: flex-start; flex-direction: column; }
      .record-controls { width: 100%; }
      .record-controls input { width: 100%; min-width: 0; }
      .history button { width: 100%; }
      .chart-wrap { height: 220px; }
    }
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
    <section class="history">
      <div class="history-head">
        <h2>已记录结果趋势</h2>
        <div class="record-controls">
          <input id="record-label" type="text" maxlength="120" placeholder="备注（可选）" aria-label="历史记录备注">
          <button id="record-result" type="button">记录本次结果</button>
        </div>
      </div>
      <p id="comparison" class="comparison">正在读取历史记录...</p>
      <div class="chart-wrap">
        <canvas id="history-chart" aria-label="总体准确率历史趋势"></canvas>
        <div id="chart-empty" class="chart-empty" hidden></div>
      </div>
    </section>
    <div class="table-wrap">
      <table>
        <thead><tr><th>结果</th><th>Case</th><th>Assistant 回复</th><th>期望决策</th><th>实际决策</th><th>期望工具</th><th>实际工具</th><th>说明</th></tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
    <footer>原始 JSON 保留 thread_id / run_id，用于关联结构化服务日志。</footer>
  </main>
  <script>
    const pageData = ${pageData};
    const recordButton = document.getElementById("record-result");
    const recordLabel = document.getElementById("record-label");
    const comparison = document.getElementById("comparison");
    const canvas = document.getElementById("history-chart");
    const emptyState = document.getElementById("chart-empty");
    let suiteRecords = [];

    function accuracy(record) {
      return Number(record.metrics?.overall ?? 0);
    }

    function describeComparison(records) {
      const currentIndex = records.findIndex(
        (record) => record.report_id === pageData.reportId,
      );
      const previous =
        currentIndex > 0
          ? records[currentIndex - 1]
          : currentIndex < 0
            ? records.at(-1)
            : null;
      if (!previous) {
        return currentIndex === 0
          ? "这是当前测试集记录的第一个节点。"
          : "当前测试集还没有已记录结果。";
      }
      const delta = pageData.overallAccuracy - accuracy(previous);
      const direction =
        delta > 0 ? "上升" : delta < 0 ? "下降" : "持平";
      return \`本次 \${pageData.overallAccuracy.toFixed(1)}%，上次记录 \${accuracy(previous).toFixed(1)}%，\${direction}\${delta === 0 ? "" : " " + Math.abs(delta).toFixed(1) + " 个百分点"}。\`;
    }

    function drawChart(records) {
      const context = canvas.getContext("2d");
      const bounds = canvas.getBoundingClientRect();
      const ratio = window.devicePixelRatio || 1;
      canvas.width = Math.max(1, Math.floor(bounds.width * ratio));
      canvas.height = Math.max(1, Math.floor(bounds.height * ratio));
      context.scale(ratio, ratio);
      context.clearRect(0, 0, bounds.width, bounds.height);

      if (records.length === 0) {
        emptyState.hidden = false;
        emptyState.textContent = "点击“记录本次结果”后，这里会出现第一个趋势节点。";
        return;
      }
      emptyState.hidden = true;
      const padding = { top: 20, right: 20, bottom: 46, left: 44 };
      const width = bounds.width - padding.left - padding.right;
      const height = bounds.height - padding.top - padding.bottom;
      context.font = "12px system-ui, sans-serif";
      context.lineWidth = 1;
      context.textAlign = "right";
      context.textBaseline = "middle";

      for (const value of [0, 25, 50, 75, 100]) {
        const y = padding.top + height * (1 - value / 100);
        context.strokeStyle = "#e4e9e7";
        context.beginPath();
        context.moveTo(padding.left, y);
        context.lineTo(padding.left + width, y);
        context.stroke();
        context.fillStyle = "#71807b";
        context.fillText(\`\${value}%\`, padding.left - 8, y);
      }

      const point = (record, index) => ({
        x:
          records.length === 1
            ? padding.left + width / 2
            : padding.left + (width * index) / (records.length - 1),
        y: padding.top + height * (1 - accuracy(record) / 100),
      });
      const points = records.map(point);
      context.strokeStyle = "#176b52";
      context.lineWidth = 2;
      context.beginPath();
      points.forEach((item, index) => {
        if (index === 0) context.moveTo(item.x, item.y);
        else context.lineTo(item.x, item.y);
      });
      context.stroke();

      points.forEach((item, index) => {
        const record = records[index];
        context.fillStyle = "#fff";
        context.strokeStyle = "#176b52";
        context.lineWidth = 2;
        context.beginPath();
        context.arc(item.x, item.y, 4, 0, Math.PI * 2);
        context.fill();
        context.stroke();

        context.fillStyle = "#173f35";
        context.textAlign = "center";
        context.textBaseline = "bottom";
        context.fillText(\`\${accuracy(record).toFixed(1)}%\`, item.x, item.y - 8);
        context.fillStyle = "#71807b";
        context.textBaseline = "top";
        const label = record.label || new Date(record.recorded_at).toLocaleDateString();
        const shortLabel = label.length > 16 ? label.slice(0, 15) + "..." : label;
        if (records.length === 1) context.textAlign = "center";
        else if (index === 0) context.textAlign = "left";
        else if (index === records.length - 1) context.textAlign = "right";
        else context.textAlign = "center";
        context.fillText(shortLabel, item.x, padding.top + height + 12);
      });
    }

    async function refreshHistory() {
      try {
        const response = await fetch("/api/history", { cache: "no-store" });
        if (!response.ok) throw new Error("history service unavailable");
        const history = await response.json();
        suiteRecords = history.records.filter(
          (record) => record.suite_fingerprint === pageData.suiteFingerprint,
        );
        const alreadyRecorded = suiteRecords.some(
          (record) => record.report_id === pageData.reportId,
        );
        recordButton.disabled = alreadyRecorded;
        recordLabel.disabled = alreadyRecorded;
        recordButton.textContent = alreadyRecorded ? "已记录" : "记录本次结果";
        comparison.textContent = describeComparison(suiteRecords);
        drawChart(suiteRecords);
      } catch {
        recordButton.disabled = true;
        comparison.textContent =
          "历史记录服务未运行。请通过双击 Benchmark 脚本打开本报告。";
        emptyState.hidden = false;
        emptyState.textContent = "无法读取历史趋势";
      }
    }

    recordButton.addEventListener("click", async () => {
      recordButton.disabled = true;
      recordButton.textContent = "记录中...";
      try {
        const response = await fetch("/api/history", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            report_id: pageData.reportId,
            label: recordLabel.value,
          }),
        });
        if (!response.ok) throw new Error("record failed");
        await refreshHistory();
      } catch {
        recordButton.disabled = false;
        recordButton.textContent = "重试记录";
        comparison.textContent = "记录失败，请确认本地报告服务仍在运行。";
      }
    });

    window.addEventListener("resize", () => drawChart(suiteRecords));
    refreshHistory();
  </script>
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
