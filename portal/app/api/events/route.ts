import { getPortalDatabase } from "../../../lib/portal-db";
import {
  cleanText,
  eventTypes,
  jsonRequestRejectionStatus,
  jsonResponse,
  readAttribution,
} from "../../../lib/portal-validation";

export async function POST(request: Request) {
  const rejectionStatus = jsonRequestRejectionStatus(request);
  if (rejectionStatus) {
    return jsonResponse({ error: "只接受同源 JSON 请求。" }, rejectionStatus);
  }

  let input: Record<string, unknown>;
  try {
    input = await request.json() as Record<string, unknown>;
  } catch {
    return jsonResponse({ error: "请求格式不正确。" }, 400);
  }

  const eventType = cleanText(input.eventType, 32);
  const sessionId = cleanText(input.sessionId, 80);
  if (!eventTypes.includes(eventType as (typeof eventTypes)[number]) || !sessionId) {
    return jsonResponse({ error: "缺少有效的事件信息。" }, 400);
  }

  const attribution = readAttribution(input.attribution);
  const now = new Date().toISOString();
  const database = await getPortalDatabase();
  await database.prepare(`
    INSERT INTO portal_events (
      id, event_type, session_id, scenario, source, medium, campaign,
      content, referrer, landing_path, created_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
  `).bind(
    crypto.randomUUID(),
    eventType,
    sessionId,
    cleanText(input.scenario, 80),
    attribution.source,
    attribution.medium,
    attribution.campaign,
    attribution.content,
    cleanText(input.referrer, 300),
    cleanText(input.landingPath, 300) || "/",
    now,
  ).run();

  return jsonResponse({ ok: true }, 201);
}
