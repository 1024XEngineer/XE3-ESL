#!/usr/bin/env bash

set -euo pipefail

fail() {
  printf 'production probe incident: %s\n' "$*" >&2
  exit 1
}

[[ $# == 2 ]] || fail 'usage: github-probe-incident.sh firing|resolved|success|failure|skipped production|drill'
readonly requested_state=$1
readonly kind=$2
case "$kind" in
  production | drill) ;;
  *) fail 'kind must be production or drill' ;;
esac
state=''
case "$requested_state" in
  firing | resolved)
    state=$requested_state
    ;;
  success)
    [[ "$kind" == production ]] || fail 'probe outcomes apply only to production'
    state=resolved
    ;;
  failure)
    [[ "$kind" == production ]] || fail 'probe outcomes apply only to production'
    state=firing
    ;;
  skipped)
    [[ "$kind" == production ]] || fail 'probe outcomes apply only to production'
    fail 'production probe did not execute; refusing to create an endpoint incident'
    ;;
  *) fail 'state must be firing, resolved, success, failure, or skipped' ;;
esac
readonly state

for name in GITHUB_REPOSITORY GITHUB_SERVER_URL GITHUB_RUN_ID GITHUB_RUN_ATTEMPT GITHUB_ACTOR; do
  [[ -n "${!name:-}" && "${!name}" != *$'\n'* && "${!name}" != *$'\r'* ]] ||
    fail "$name is required and must be single-line"
done
[[ "$GITHUB_REPOSITORY" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] ||
  fail 'GITHUB_REPOSITORY is invalid'
[[ "$GITHUB_RUN_ID" =~ ^[0-9]+$ && "$GITHUB_RUN_ATTEMPT" =~ ^[0-9]+$ ]] ||
  fail 'GitHub run identifiers must be numeric'

command -v gh >/dev/null 2>&1 || fail 'gh is required'
command -v jq >/dev/null 2>&1 || fail 'jq is required'

if [[ "$kind" == production ]]; then
  readonly title='[production-probe] SpeakUp public endpoint incident'
else
  readonly title='[production-probe drill] SpeakUp notification drill'
fi
readonly run_url="$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID/attempts/$GITHUB_RUN_ATTEMPT"
readonly occurred_at="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
readonly incident_marker='<!-- speakup-production-probe-incident:v1 -->'

issues_json=$(gh api --paginate --slurp \
  "repos/$GITHUB_REPOSITORY/issues?state=open&per_page=100")
issue_number=$(jq -er --arg title "$title" --arg marker "$incident_marker" '
  [.[][] | select(
    (has("pull_request") | not) and
    .title == $title and
    ((.body // "") | contains($marker))
  )] |
  if length == 0 then ""
  elif length == 1 then (.[0].number | tostring)
  else error("multiple matching probe incidents")
  end
' <<<"$issues_json") || fail 'open incident response is invalid or ambiguous'

if [[ "$state" == firing ]]; then
  if [[ -z "$issue_number" ]]; then
    body=$(jq -n \
      --arg title "$title" \
      --arg run_url "$run_url" \
      --arg occurred_at "$occurred_at" \
      --arg actor "$GITHUB_ACTOR" \
      --arg marker "$incident_marker" \
      '{
        title: $title,
        body: ($marker + "\n\nThe off-host SpeakUp production probe entered **firing** state.\n\n- observed_at: `" + $occurred_at + "`\n- workflow_run: " + $run_url + "\n- actor: `" + $actor + "`\n\nThis issue will be closed automatically after a successful probe or explicit drill resolution."),
        assignees: ["Lq0412"]
      }')
    issue_number=$(gh api --method POST "repos/$GITHUB_REPOSITORY/issues" \
      --input - --jq '.number' <<<"$body")
    [[ "$issue_number" =~ ^[0-9]+$ ]] || fail 'created incident number is invalid'
    printf 'Opened incident #%s: %s\n' "$issue_number" "$run_url" >>"$GITHUB_STEP_SUMMARY"
  else
    printf 'Incident #%s remains open: %s\n' "$issue_number" "$run_url" >>"$GITHUB_STEP_SUMMARY"
  fi
  exit 0
fi

if [[ -z "$issue_number" ]]; then
  [[ "$kind" == production ]] || fail 'drill resolution requires an open drill incident'
  printf 'No open production incident required resolution.\n' >>"$GITHUB_STEP_SUMMARY"
  exit 0
fi

comment=$(jq -n \
  --arg occurred_at "$occurred_at" \
  --arg run_url "$run_url" \
  '{body: ("The off-host probe entered **resolved** state.\n\n- observed_at: `" + $occurred_at + "`\n- workflow_run: " + $run_url)}')
gh api --method POST \
  "repos/$GITHUB_REPOSITORY/issues/$issue_number/comments" --input - <<<"$comment" >/dev/null
gh api --method PATCH "repos/$GITHUB_REPOSITORY/issues/$issue_number" \
  --input - >/dev/null <<'JSON'
{"state":"closed","state_reason":"completed"}
JSON
printf 'Resolved incident #%s: %s\n' "$issue_number" "$run_url" >>"$GITHUB_STEP_SUMMARY"
