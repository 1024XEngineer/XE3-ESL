import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { loadHistory, recordReport } from "./history.mjs";

test("records a report once and preserves its metric snapshot", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "routing-history-"));
  const reportId = "2026-07-29T100000-000Z";
  await writeFile(
    path.join(directory, `${reportId}.json`),
    JSON.stringify({
      metadata: {
        report_id: reportId,
        generated_at: "2026-07-29T10:00:00.000Z",
        git_revision: "abc1234-dirty",
        suite_fingerprint: "sha256:test",
        provider: "test",
        model: "test-model",
      },
      metrics: {
        overall: { percentage: 70 },
        decision: { percentage: 80 },
      },
    }),
  );

  const first = await recordReport(directory, reportId, "prompt v2");
  const second = await recordReport(directory, reportId, "ignored");
  const history = await loadHistory(directory);

  assert.equal(first.created, true);
  assert.equal(second.created, false);
  assert.equal(history.records.length, 1);
  assert.equal(history.records[0].label, "prompt v2");
  assert.equal(history.records[0].metrics.overall, 70);
  assert.equal(
    JSON.parse(
      await readFile(
        path.join(directory, "history", "snapshots", `${reportId}.json`),
        "utf8",
      ),
    ).metadata.report_id,
    reportId,
  );
});

test("rejects unknown report ids", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "routing-history-"));
  await assert.rejects(
    recordReport(directory, "../outside", ""),
    /invalid report id/,
  );
});
