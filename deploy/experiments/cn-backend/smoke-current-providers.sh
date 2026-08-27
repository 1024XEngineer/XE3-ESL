#!/usr/bin/env bash
set -euo pipefail

readonly base_url="http://127.0.0.1:28083"
session_token=""

cleanup() {
  if [[ -n "$session_token" ]]; then
    curl --silent --show-error --max-time 10 \
      --request POST \
      --header "Authorization: Bearer $session_token" \
      "$base_url/v1/auth/logout" >/dev/null || true
  fi
}
trap cleanup EXIT

for command in curl jq openssl; do
  command -v "$command" >/dev/null 2>&1 || {
    printf '%s is required\n' "$command" >&2
    exit 1
  }
done

request_body=""
request_code=""
request_seconds=""

request_json() {
  local method="$1"
  local path="$2"
  local payload="${3:-}"
  local token="${4:-}"
  local result metrics
  local -a arguments=(
    --silent
    --show-error
    --max-time 180
    --request "$method"
    --write-out $'\n%{http_code} %{time_total}'
  )

  if [[ -n "$payload" ]]; then
    arguments+=(--header 'Content-Type: application/json' --data "$payload")
  fi
  if [[ -n "$token" ]]; then
    arguments+=(--header "Authorization: Bearer $token")
  fi

  result="$(curl "${arguments[@]}" "$base_url$path")"
  metrics="${result##*$'\n'}"
  request_body="${result%$'\n'*}"
  read -r request_code request_seconds <<<"$metrics"
}

require_code() {
  local operation="$1"
  local expected="$2"
  if [[ "$request_code" != "$expected" ]]; then
    printf '%s returned HTTP %s, expected %s\n' \
      "$operation" "$request_code" "$expected" >&2
    exit 1
  fi
  printf '%s http=%s seconds=%s\n' \
    "$operation" "$request_code" "$request_seconds"
}

email="cn-experiment-$(date +%s)-$(openssl rand -hex 4)@example.com"
password="$(openssl rand -hex 18)"
register_payload="$(jq -cn \
  --arg email "$email" \
  --arg password "$password" \
  '{email: $email, password: $password, display_name: "CN Experiment"}')"

request_json POST /v1/auth/register "$register_payload"
require_code register 201

login_payload="$(jq -cn \
  --arg email "$email" \
  --arg password "$password" \
  '{email: $email, password: $password}')"
request_json POST /v1/auth/login "$login_payload"
require_code login 200
session_token="$(jq -er '.session_token' <<<"$request_body")"

request_json GET /v1/me '' "$session_token"
require_code current_user 200

request_json GET /v1/ielts-speaking/question-bank
require_code question_bank 200
bank_id="$(jq -er '.bank_id' <<<"$request_body")"
source_id="$(jq -er '.part1_topics[0].id' <<<"$request_body")"
bank_path="$(jq -nr --arg value "$bank_id" '$value | @uri')"
source_path="$(jq -nr --arg value "$source_id" '$value | @uri')"

answer_payload="$(jq -cn \
  --arg bank "$bank_id" \
  --arg source "$source_id" \
  '{
    question: {
      bank_id: $bank,
      part: "PART_1",
      source_id: $source,
      question_position: 1
    },
    personal_points: [],
    target_band: 6.5
  }')"
request_json POST /v1/ielts-speaking/answers:generate \
  "$answer_payload" "$session_token"
require_code answer_generation 200
jq -e '
  (.answer | type == "string" and length > 0) and
  (.speech_text | type == "string" and length > 0)
' <<<"$request_body" >/dev/null

tts_metrics="$(curl \
  --silent \
  --show-error \
  --max-time 90 \
  --output /dev/null \
  --write-out '%{http_code} %{time_total} %{size_download} %{content_type}' \
  --header "Authorization: Bearer $session_token" \
  "$base_url/v1/ielts-speaking/question-banks/$bank_path/PART_1/$source_path/questions/1/speech")"
read -r tts_code tts_seconds tts_bytes tts_content_type <<<"$tts_metrics"
if [[ "$tts_code" != 200 || "$tts_bytes" -le 44 ||
      "$tts_content_type" != audio/wav* ]]; then
  printf 'question_speech response is invalid: %s\n' "$tts_metrics" >&2
  exit 1
fi
printf 'question_speech http=%s seconds=%s bytes=%s content_type=%s\n' \
  "$tts_code" "$tts_seconds" "$tts_bytes" "$tts_content_type"

request_json POST /v1/auth/logout '' "$session_token"
require_code logout 204
session_token=""

printf 'Current-provider smoke passed; the test account remains only in the isolated experiment database.\n'
