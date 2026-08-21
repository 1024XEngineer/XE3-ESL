import { isAdminRequest } from "../../../../lib/admin-auth";
import { getPortalDatabase } from "../../../../lib/portal-db";
import { jsonResponse } from "../../../../lib/portal-validation";

function csvCell(value: unknown): string {
  const text = value == null ? "" : String(value);
  const safeText = /^[=+\-@\t\r]/.test(text) ? `'${text}` : text;
  return `"${safeText.replaceAll('"', '""')}"`;
}

export async function GET(request: Request) {
  if (!await isAdminRequest(request)) {
    return jsonResponse({ error: "密码不正确，或管理密码尚未配置。" }, 401);
  }

  const database = await getPortalDatabase();
  const result = await database.prepare(`
    SELECT scenario, urgency, target_role, challenge, contact, source,
           medium, campaign, content, created_at, updated_at
    FROM portal_waitlist ORDER BY created_at DESC
  `).all<Record<string, unknown>>();

  const headers = [
    "scenario", "urgency", "target_role", "challenge", "contact",
    "source", "medium", "campaign", "content", "created_at", "updated_at",
  ];
  const rows = (result.results ?? []).map((row) =>
    headers.map((header) => csvCell(row[header])).join(","),
  );
  const csv = `\uFEFF${headers.join(",")}\n${rows.join("\n")}\n`;

  return new Response(csv, {
    headers: {
      "Cache-Control": "no-store",
      "Content-Disposition": 'attachment; filename="speakup-waitlist.csv"',
      "Content-Type": "text/csv; charset=utf-8",
      "X-Content-Type-Options": "nosniff",
    },
  });
}
