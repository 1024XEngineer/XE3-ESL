import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";

const historyVersion = 1;

async function readJson(file, fallback) {
  try {
    return JSON.parse(await readFile(file, "utf8"));
  } catch (error) {
    if (error.code === "ENOENT") return fallback;
    throw error;
  }
}

function metricPercentages(metrics) {
  return Object.fromEntries(
    Object.entries(metrics).map(([name, metric]) => [
      name,
      Number(metric.percentage),
    ]),
  );
}

export function historyPaths(reportDirectory) {
  const directory = path.join(reportDirectory, "history");
  return {
    directory,
    index: path.join(directory, "index.json"),
    snapshots: path.join(directory, "snapshots"),
  };
}

export async function loadHistory(reportDirectory) {
  const paths = historyPaths(reportDirectory);
  const data = await readJson(paths.index, {
    version: historyVersion,
    records: [],
  });
  if (data.version !== historyVersion || !Array.isArray(data.records)) {
    throw new Error("unsupported benchmark history format");
  }
  return data;
}

async function writeHistoryAtomic(indexFile, data) {
  const temporary = `${indexFile}.${process.pid}.tmp`;
  await writeFile(temporary, `${JSON.stringify(data, null, 2)}\n`);
  await rename(temporary, indexFile);
}

export async function recordReport(reportDirectory, reportId, label = "") {
  if (!/^[A-Za-z0-9._:-]{1,128}$/.test(reportId)) {
    throw new Error("invalid report id");
  }
  const normalizedLabel = String(label).trim();
  if (normalizedLabel.length > 120) {
    throw new Error("label must not exceed 120 characters");
  }

  const reportFile = path.join(reportDirectory, `${reportId}.json`);
  const report = await readJson(reportFile, null);
  if (!report || report.metadata?.report_id !== reportId) {
    throw new Error("benchmark report not found");
  }
  const paths = historyPaths(reportDirectory);
  await mkdir(paths.snapshots, { recursive: true });
  const history = await loadHistory(reportDirectory);
  const existing = history.records.find(
    (record) => record.report_id === reportId,
  );
  if (existing) {
    return { history, record: existing, created: false };
  }

  const record = {
    report_id: reportId,
    recorded_at: new Date().toISOString(),
    generated_at: report.metadata.generated_at,
    label: normalizedLabel,
    git_revision: report.metadata.git_revision ?? "",
    suite_fingerprint: report.metadata.suite_fingerprint ?? "",
    provider: report.metadata.provider ?? "",
    model: report.metadata.model ?? "",
    metrics: metricPercentages(report.metrics),
  };
  history.records.push(record);
  history.records.sort((left, right) =>
    left.recorded_at.localeCompare(right.recorded_at),
  );

  await Promise.all([
    writeFile(
      path.join(paths.snapshots, `${reportId}.json`),
      `${JSON.stringify(report, null, 2)}\n`,
    ),
    writeHistoryAtomic(paths.index, history),
  ]);
  return { history, record, created: true };
}
