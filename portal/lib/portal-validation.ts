export const eventTypes = ["page_view", "cta_click"] as const;
export type EventType = (typeof eventTypes)[number];

export interface Attribution {
  source: string;
  medium: string;
  campaign: string;
  content: string;
}

export function cleanText(value: unknown, maxLength: number): string {
  return typeof value === "string"
    ? value.replace(/\s+/g, " ").trim().slice(0, maxLength)
    : "";
}

export function readAttribution(value: unknown): Attribution {
  const candidate = value && typeof value === "object"
    ? value as Record<string, unknown>
    : {};

  return {
    source: cleanText(candidate.source, 100),
    medium: cleanText(candidate.medium, 100),
    campaign: cleanText(candidate.campaign, 140),
    content: cleanText(candidate.content, 140),
  };
}

export function jsonRequestRejectionStatus(request: Request): 403 | 415 | null {
  const mediaType = request.headers.get("content-type")
    ?.split(";", 1)[0]
    .trim()
    .toLowerCase();
  if (mediaType !== "application/json") return 415;

  const origin = request.headers.get("origin");
  if (origin) {
    try {
      if (new URL(origin).origin !== new URL(request.url).origin) return 403;
    } catch {
      return 403;
    }
  }

  if (request.headers.get("sec-fetch-site") === "cross-site") return 403;
  return null;
}

export function jsonResponse(body: unknown, status = 200): Response {
  return Response.json(body, {
    status,
    headers: {
      "Cache-Control": "no-store",
      "X-Content-Type-Options": "nosniff",
    },
  });
}
