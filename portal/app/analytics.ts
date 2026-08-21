"use client";

export interface PortalAttribution {
  source: string;
  medium: string;
  campaign: string;
  content: string;
}

const sessionKey = "speakup_portal_session";

export function getPortalSessionId(): string {
  const existing = window.sessionStorage.getItem(sessionKey);
  if (existing) return existing;
  const created = crypto.randomUUID();
  window.sessionStorage.setItem(sessionKey, created);
  return created;
}

export function getPortalAttribution(): PortalAttribution {
  const params = new URLSearchParams(window.location.search);
  return {
    source: params.get("utm_source") ?? "",
    medium: params.get("utm_medium") ?? "",
    campaign: params.get("utm_campaign") ?? "",
    content: params.get("utm_content") ?? "",
  };
}

export function trackPortalEvent(
  eventType: "page_view" | "cta_click",
  scenario = "",
): void {
  void fetch("/api/events", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      eventType,
      scenario,
      sessionId: getPortalSessionId(),
      attribution: getPortalAttribution(),
      referrer: document.referrer,
      landingPath: `${window.location.pathname}${window.location.search}`,
    }),
    keepalive: true,
  }).catch(() => undefined);
}
