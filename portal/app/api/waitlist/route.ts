import { getPortalDatabase } from "../../../lib/portal-db";
import {
  cleanText,
  jsonRequestRejectionStatus,
  jsonResponse,
  readAttribution,
} from "../../../lib/portal-validation";

const allowedScenarios = new Set(["英文面试", "雅思口语", "海外日常", "国际职场", "其他"]);
const allowedUrgencies = new Set(["两周内", "一个月内", "三个月内", "先了解"]);

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

  if (cleanText(input.website, 120)) {
    return jsonResponse({ ok: true }, 201);
  }

  const sessionId = cleanText(input.sessionId, 80);
  const scenario = cleanText(input.scenario, 80);
  const urgency = cleanText(input.urgency, 40);
  const contact = cleanText(input.contact, 160);
  const consent = input.consent === true;

  if (
    !sessionId ||
    !allowedScenarios.has(scenario) ||
    !allowedUrgencies.has(urgency) ||
    contact.length < 3 ||
    !consent
  ) {
    return jsonResponse({ error: "请完整填写必填信息并同意联系说明。" }, 400);
  }

  const attribution = readAttribution(input.attribution);
  const now = new Date().toISOString();
  const id = crypto.randomUUID();
  const contactNormalized = contact.toLowerCase();
  const database = await getPortalDatabase();

  await database.batch([
    database.prepare(`
      INSERT INTO portal_waitlist (
        id, session_id, scenario, urgency, target_role, challenge, contact,
        contact_normalized, source, medium, campaign, content, consent,
        created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
      ON CONFLICT(contact_normalized) DO UPDATE SET
        scenario = excluded.scenario,
        urgency = excluded.urgency,
        target_role = excluded.target_role,
        challenge = excluded.challenge,
        contact = excluded.contact,
        source = excluded.source,
        medium = excluded.medium,
        campaign = excluded.campaign,
        content = excluded.content,
        updated_at = excluded.updated_at
      WHERE portal_waitlist.session_id = excluded.session_id
    `).bind(
      id,
      sessionId,
      scenario,
      urgency,
      cleanText(input.targetRole, 160),
      cleanText(input.challenge, 500),
      contact,
      contactNormalized,
      attribution.source,
      attribution.medium,
      attribution.campaign,
      attribution.content,
      now,
      now,
    ),
    database.prepare(`
      INSERT INTO portal_events (
        id, event_type, session_id, scenario, source, medium, campaign,
        content, referrer, landing_path, created_at
      )
      SELECT ?, 'signup_submit', ?, ?, ?, ?, ?, ?, '', '/', ?
      WHERE EXISTS (
        SELECT 1 FROM portal_waitlist
        WHERE contact_normalized = ? AND session_id = ?
      )
      AND NOT EXISTS (
        SELECT 1 FROM portal_events
        WHERE event_type = 'signup_submit' AND session_id = ?
      )
    `).bind(
      crypto.randomUUID(),
      sessionId,
      scenario,
      attribution.source,
      attribution.medium,
      attribution.campaign,
      attribution.content,
      now,
      contactNormalized,
      sessionId,
      sessionId,
    ),
  ]);

  return jsonResponse({ ok: true }, 201);
}
