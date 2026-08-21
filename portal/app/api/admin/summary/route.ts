import { isAdminRequest } from "../../../../lib/admin-auth";
import { getPortalDatabase } from "../../../../lib/portal-db";
import { jsonResponse } from "../../../../lib/portal-validation";

interface CountRow {
  count: number;
}

export async function GET(request: Request) {
  if (!await isAdminRequest(request)) {
    return jsonResponse({ error: "密码不正确，或管理密码尚未配置。" }, 401);
  }

  const database = await getPortalDatabase();
  const [views, clicks, submissions, recent, sources, daily] = await Promise.all([
    database.prepare("SELECT COUNT(DISTINCT session_id) AS count FROM portal_events WHERE event_type = 'page_view'").first<CountRow>(),
    database.prepare("SELECT COUNT(DISTINCT session_id) AS count FROM portal_events WHERE event_type = 'cta_click'").first<CountRow>(),
    database.prepare("SELECT COUNT(*) AS count FROM portal_waitlist").first<CountRow>(),
    database.prepare(`
      SELECT id, scenario, urgency, target_role, challenge, contact, source, campaign, created_at
      FROM portal_waitlist ORDER BY created_at DESC LIMIT 50
    `).all(),
    database.prepare(`
      SELECT CASE WHEN source = '' THEN '直接访问' ELSE source END AS source,
             COUNT(DISTINCT session_id) AS visits
      FROM portal_events WHERE event_type = 'page_view'
      GROUP BY source ORDER BY visits DESC LIMIT 10
    `).all(),
    database.prepare(`
      SELECT substr(created_at, 1, 10) AS day,
             COUNT(DISTINCT CASE WHEN event_type = 'page_view' THEN session_id END) AS views,
             COUNT(DISTINCT CASE WHEN event_type = 'cta_click' THEN session_id END) AS clicks,
             COUNT(DISTINCT CASE WHEN event_type = 'signup_submit' THEN session_id END) AS submissions
      FROM portal_events
      WHERE created_at >= datetime('now', '-13 days')
      GROUP BY substr(created_at, 1, 10)
      ORDER BY day
    `).all(),
  ]);

  const viewCount = Number(views?.count ?? 0);
  const clickCount = Number(clicks?.count ?? 0);
  const submissionCount = Number(submissions?.count ?? 0);

  return jsonResponse({
    funnel: {
      views: viewCount,
      clicks: clickCount,
      submissions: submissionCount,
      clickRate: viewCount ? clickCount / viewCount : 0,
      submitRate: clickCount ? submissionCount / clickCount : 0,
    },
    recent: recent.results ?? [],
    sources: sources.results ?? [],
    daily: daily.results ?? [],
  });
}
