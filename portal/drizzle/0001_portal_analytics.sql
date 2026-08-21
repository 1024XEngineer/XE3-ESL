CREATE TABLE IF NOT EXISTS portal_events (
  id TEXT PRIMARY KEY,
  event_type TEXT NOT NULL CHECK (event_type IN ('page_view', 'cta_click', 'signup_submit')),
  session_id TEXT NOT NULL,
  scenario TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  medium TEXT NOT NULL DEFAULT '',
  campaign TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  referrer TEXT NOT NULL DEFAULT '',
  landing_path TEXT NOT NULL DEFAULT '/',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS portal_waitlist (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  scenario TEXT NOT NULL,
  urgency TEXT NOT NULL,
  target_role TEXT NOT NULL DEFAULT '',
  challenge TEXT NOT NULL DEFAULT '',
  contact TEXT NOT NULL,
  contact_normalized TEXT NOT NULL UNIQUE,
  source TEXT NOT NULL DEFAULT '',
  medium TEXT NOT NULL DEFAULT '',
  campaign TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  consent INTEGER NOT NULL CHECK (consent = 1),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS portal_events_type_date_idx
  ON portal_events (event_type, created_at);
CREATE INDEX IF NOT EXISTS portal_events_session_idx
  ON portal_events (session_id);
CREATE INDEX IF NOT EXISTS portal_waitlist_date_idx
  ON portal_waitlist (created_at);
