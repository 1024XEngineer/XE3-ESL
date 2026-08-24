#!/usr/bin/env bash

set -euo pipefail

readonly observability_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly compose_file="$observability_directory/compose.yaml"
readonly exporter="$observability_directory/xe3-speakup-export-metrics"
readonly rules_file="$observability_directory/rules/speakup.yml"
readonly logrotate_file="$observability_directory/xe3-speakup-nginx.logrotate"
readonly dashboard_file="$observability_directory/grafana/dashboards/speakup-overview.json"
readonly monitor_nginx="$observability_directory/monitor-nginx.conf"
readonly private_file_validator="$observability_directory/validate-private-files"
readonly product_health_reader="$observability_directory/configure-product-health-reader"
readonly probe_incident="$observability_directory/github-probe-incident.sh"
readonly probe_workflow="$observability_directory/../../.github/workflows/production-probe.yml"
readonly metrics_service="$observability_directory/xe3-speakup-observability-metrics.service"
readonly metrics_environment_example="$observability_directory/observability-metrics.env.example"
readonly product_health_dashboard="$observability_directory/grafana/dashboards/speakup-product-health.json"
readonly product_health_datasource="$observability_directory/grafana/provisioning/datasources/product-health.yml"

fail() {
  printf 'observability contract test: %s\n' "$*" >&2
  exit 1
}

expect_failure() {
  local name=$1
  shift
  if "$@" >"$temporary_directory/failure.out" 2>&1; then
    fail "$name unexpectedly succeeded"
  fi
}

temporary_directory=$(mktemp -d)
temporary_directory=$(cd "$temporary_directory" && pwd -P)
readonly temporary_directory
trap 'rm -rf "$temporary_directory"' EXIT

mkdir -p \
  "$temporary_directory/fake-bin" \
  "$temporary_directory/postgres-backups/20260824T010203Z-daily" \
  "$temporary_directory/portal-backups/20260824T020304Z" \
  "$temporary_directory/textfile"
printf '%s\n' 'fixture certificate' >"$temporary_directory/production.pem"
printf '%s\n' 'fixture certificate' >"$temporary_directory/staging.pem"
printf '%s\n' 'fixture certificate' >"$temporary_directory/monitor.pem"
printf '%s\n' 'fixture password' >"$temporary_directory/grafana-password"
printf '%s\n' \
  'postgres:5432:speakup:speakup_product_health_reader:ProductHealthReader_1234567890abcdef' \
  >"$temporary_directory/product-health.pgpass"
printf '%s\n' \
  '{"created_at":"2026-08-24T01:02:03Z"}' \
  >"$temporary_directory/postgres-backups/20260824T010203Z-daily/metadata.json"
printf '%s\n' \
  '{"created_at":"2026-08-24T02:03:04.110Z"}' \
  >"$temporary_directory/portal-backups/20260824T020304Z/metadata.json"

cat >"$temporary_directory/fake-bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  ps)
    project=''
    service=''
    while (($# > 0)); do
      if [[ "$1" == --filter ]]; then
        case "$2" in
          label=com.docker.compose.project=*)
            project=${2#label=com.docker.compose.project=}
            ;;
          label=com.docker.compose.service=*)
            service=${2#label=com.docker.compose.service=}
            ;;
        esac
        shift 2
      else
        shift
      fi
    done
    if [[ "$service" != "${MISSING_SERVICE:-}" ]]; then
      printf '%s--%s\n' "$project" "$service"
    fi
    ;;
  inspect)
    service=${2##*--}
    restarts=0
    [[ "$service" != server ]] || restarts=2
    printf '[{"State":{"Running":true,"Health":{"Status":"healthy"}},"RestartCount":%s}]\n' "$restarts"
    ;;
  *) exit 2 ;;
esac
EOF

cat >"$temporary_directory/fake-bin/openssl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' 'notAfter=Nov 21 00:00:00 2026 GMT'
EOF

cat >"$temporary_directory/fake-bin/date" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' '1795219200'
EOF

cat >"$temporary_directory/fake-bin/stat" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[[ $# == 4 && "$1" == -c && "$3" == -- ]] || exit 2
readonly format=$2
readonly path=$4
readonly state_directory=/var/lib/speakup/safety-checks

if [[ "$path" == "$state_directory" && "$format" == '%f:%u:%g:%a' ]]; then
  [[ "${MISSING_SAFETY_DIRECTORY:-0}" != 1 ]] || exit 1
  if [[ "${UNSAFE_SAFETY_DIRECTORY:-0}" == 1 ]]; then
    printf '%s\n' '41ed:0:0:755'
  else
    printf '%s\n' '41c0:0:0:700'
  fi
  exit
fi

[[ "$path" == "$state_directory/"* && "$format" == '%f:%u:%g:%a:%Y' ]] || exit 2
readonly marker=${path##*/}
[[ "$marker" != "${MISSING_SAFETY_MARKER:-}" ]] || exit 1

case "$marker" in
  postgres-restore-check.success) timestamp=1787533323 ;;
  portal-sqlite-restore-check.success) timestamp=1787536984 ;;
  tls-renewal.success) timestamp=1787540585 ;;
  *) exit 2 ;;
esac

if [[ "$marker" == "${UNSAFE_SAFETY_MARKER:-}" ]]; then
  printf '81b6:0:0:666:'
elif [[ "$marker" == "${NONREGULAR_SAFETY_MARKER:-}" ]]; then
  printf 'a1ff:0:0:777:'
else
  printf '8180:0:0:600:'
fi
if [[ "$marker" == "${FUTURE_SAFETY_MARKER:-}" ]]; then
  printf '%s\n' '1893456000'
else
  printf '%s\n' "$timestamp"
fi
EOF

cat >"$temporary_directory/fake-bin/df" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  '--block-size=1 --output=size,avail,target /')
    printf '%s\n' '1B-blocks Avail Mounted on' '1000000 250000 /'
    ;;
  '--output=itotal,iavail,target /')
    printf '%s\n' 'Inodes IFree Mounted on' '2000 500 /'
    ;;
  *) exit 2 ;;
esac
EOF
chmod 0755 "$temporary_directory/fake-bin/"*

bash -n "$exporter" "$private_file_validator" "$product_health_reader" \
  "$probe_incident" "$0"

export OBSERVABILITY_PRODUCTION_CERTIFICATE="$temporary_directory/production.pem"
export OBSERVABILITY_STAGING_CERTIFICATE="$temporary_directory/staging.pem"
export OBSERVABILITY_MONITOR_CERTIFICATE="$temporary_directory/monitor.pem"
export OBSERVABILITY_POSTGRES_BACKUP_ROOT="$temporary_directory/postgres-backups"
export OBSERVABILITY_PORTAL_BACKUP_ROOT="$temporary_directory/portal-backups"

readonly metrics_file="$temporary_directory/textfile/speakup.prom"
PATH="$temporary_directory/fake-bin:$PATH" "$exporter" --output "$metrics_file"

assert_safety_metric() {
  local purpose=$1
  local unit=$2
  local success=$3
  local timestamp=$4
  grep -Fq \
    "speakup_systemd_unit_last_run_success{purpose=\"$purpose\",unit=\"$unit\"} $success" \
    "$metrics_file" || fail "$purpose success metric is wrong"
  grep -Fq \
    "speakup_systemd_unit_last_run_timestamp_seconds{purpose=\"$purpose\",unit=\"$unit\"} $timestamp" \
    "$metrics_file" || fail "$purpose timestamp metric is wrong"
}

[[ $(grep -c '^speakup_container_up{' "$metrics_file") -eq 6 ]] ||
  fail 'expected six bounded container-up series'
[[ $(grep -c '^speakup_container_health{' "$metrics_file") -eq 6 ]] ||
  fail 'expected six bounded container-health series'
grep -Fq 'speakup_container_restarts_total{environment="production",service="server"} 2' \
  "$metrics_file" || fail 'container restart count was not exported'
grep -Fq 'speakup_tls_certificate_expiry_timestamp_seconds{environment="production",lineage="speak-up.top"} 1795219200' \
  "$metrics_file" || fail 'production certificate expiry was not exported'
grep -Fq 'speakup_tls_certificate_expiry_timestamp_seconds{environment="production",lineage="monitor.speak-up.top"} 1795219200' \
  "$metrics_file" || fail 'monitor certificate expiry was not exported'
grep -Fq 'speakup_backup_last_success_timestamp_seconds{database="postgres"} 1787533323' \
  "$metrics_file" || fail 'PostgreSQL backup time was not exported'
grep -Fq 'speakup_backup_last_success_timestamp_seconds{database="portal_sqlite"} 1787536984' \
  "$metrics_file" || fail 'Portal backup time was not exported'
[[ $(grep -c '^speakup_systemd_unit_last_run_success{' "$metrics_file") -eq 3 ]] ||
  fail 'expected three bounded systemd safety series'
[[ $(grep -c '^speakup_systemd_unit_last_run_timestamp_seconds{' "$metrics_file") -eq 3 ]] ||
  fail 'expected three bounded systemd last-run timestamps'
assert_safety_metric postgres_restore_check xe3-postgres-restore-check.service 1 1787533323
assert_safety_metric portal_restore_check xe3-portal-sqlite-restore-check.service 1 1787536984
assert_safety_metric tls_renewal xe3-speakup-tls-renew.service 1 1787540585
! grep -Eq 'systemctl|journalctl' "$exporter" ||
  fail 'safety-check metrics still depend on volatile systemd state or logs'
grep -Fq 'speakup_host_filesystem_avail_bytes{mountpoint="/"} 250000' "$metrics_file" ||
  fail 'host root free bytes were not exported'
grep -Fq 'speakup_host_filesystem_files_free{mountpoint="/"} 500' "$metrics_file" ||
  fail 'host root free inodes were not exported'

MISSING_SAFETY_MARKER=postgres-restore-check.success \
  PATH="$temporary_directory/fake-bin:$PATH" \
  "$exporter" --output "$metrics_file"
assert_safety_metric postgres_restore_check xe3-postgres-restore-check.service 0 0
assert_safety_metric portal_restore_check xe3-portal-sqlite-restore-check.service 1 1787536984

UNSAFE_SAFETY_MARKER=portal-sqlite-restore-check.success \
  PATH="$temporary_directory/fake-bin:$PATH" \
  "$exporter" --output "$metrics_file"
assert_safety_metric portal_restore_check xe3-portal-sqlite-restore-check.service 0 0

NONREGULAR_SAFETY_MARKER=postgres-restore-check.success \
  PATH="$temporary_directory/fake-bin:$PATH" \
  "$exporter" --output "$metrics_file"
assert_safety_metric postgres_restore_check xe3-postgres-restore-check.service 0 0

FUTURE_SAFETY_MARKER=tls-renewal.success \
  PATH="$temporary_directory/fake-bin:$PATH" \
  "$exporter" --output "$metrics_file"
assert_safety_metric tls_renewal xe3-speakup-tls-renew.service 0 0

UNSAFE_SAFETY_DIRECTORY=1 PATH="$temporary_directory/fake-bin:$PATH" \
  "$exporter" --output "$metrics_file"
[[ $(grep -c '^speakup_systemd_unit_last_run_success{.*} 0$' "$metrics_file") -eq 3 ]] ||
  fail 'unsafe safety-check state directory did not fail closed'
[[ $(grep -c '^speakup_systemd_unit_last_run_timestamp_seconds{.*} 0$' "$metrics_file") -eq 3 ]] ||
  fail 'unsafe safety-check state directory timestamps were not explicit'

MISSING_SERVICE=portal PATH="$temporary_directory/fake-bin:$PATH" \
  "$exporter" --output "$metrics_file"
[[ $(grep -c 'service="portal"} 0$' "$metrics_file") -eq 6 ]] ||
  fail 'missing Portal containers did not fail closed'

expect_failure 'relative output path' env PATH="$temporary_directory/fake-bin:$PATH" \
  "$exporter" --output relative/speakup.prom

printf '%s\n' '{"created_at":"not-an-rfc3339-time"}' \
  >"$temporary_directory/portal-backups/20260824T020304Z/metadata.json"
expect_failure 'invalid backup timestamp' env PATH="$temporary_directory/fake-bin:$PATH" \
  "$exporter" --output "$metrics_file"

OBSERVABILITY_ALERTMANAGER_CONFIG="$observability_directory/alertmanager.example.yml" \
OBSERVABILITY_GRAFANA_HOST=monitor.speak-up.top \
OBSERVABILITY_GRAFANA_ADMIN_USER=speakup-admin \
OBSERVABILITY_GRAFANA_ADMIN_PASSWORD_FILE="$temporary_directory/grafana-password" \
OBSERVABILITY_PRODUCT_HEALTH_DATABASE=speakup \
OBSERVABILITY_PRODUCT_HEALTH_PGPASS_FILE="$temporary_directory/product-health.pgpass" \
  docker compose \
    --env-file /dev/null \
    --file "$compose_file" \
    config --format json >"$temporary_directory/compose.json"

jq --exit-status \
  --arg product_health_pgpass_file "$temporary_directory/product-health.pgpass" '
  .name == "xe3-speakup-observability" and
  (.services | keys | sort) ==
    ["alertmanager", "blackbox", "grafana", "node-exporter", "prometheus"] and
  ([.services[] | .read_only == true] | all) and
  ([.services[] | .restart == "unless-stopped"] | all) and
  ([.services[] |
    .logging.driver == "json-file" and
    .logging.options["max-size"] == "10m" and
    .logging.options["max-file"] == "5"
  ] | all) and
  ([.services[] | (.cap_drop // []) == ["ALL"]] | all) and
  ([.services[] | (.security_opt // []) == ["no-new-privileges:true"]] | all) and
  ([.services[] | has("healthcheck")] | all) and
  ([.services[] | (.volumes // [])[]? | select(.source == "/var/run/docker.sock")] | length) == 0 and
  ([.services[] | (.volumes // [])[]? | select(.source == "/")] | length) == 0 and
  .services["node-exporter"].command == [
    "--collector.disable-defaults",
    "--collector.textfile",
    "--collector.textfile.directory=/textfile"
  ] and
  .services["node-exporter"].volumes[0].source == "/var/lib/xe3-speakup-observability/textfile" and
  (.services | to_entries | map(select(.key != "grafana") | (.value | has("ports"))) | any | not) and
  .services.grafana.ports[0].host_ip == "127.0.0.1" and
  (.services.grafana.ports[0].published | tostring) == "13000" and
  .services.grafana.platform == "linux/amd64" and
  .services.grafana.environment.GF_AUTH_ANONYMOUS_ENABLED == "false" and
  .services.grafana.environment.GF_USERS_DEFAULT_LANGUAGE == "zh-Hans" and
  .services.grafana.environment.PGPASSFILE == "/run/secrets/product_health_reader_pgpass" and
  .services.grafana.environment.OBSERVABILITY_PRODUCT_HEALTH_DATABASE == "speakup" and
  ([.services.grafana.secrets[] |
    select(.source == "product_health_reader_pgpass" and
      .target == "/run/secrets/product_health_reader_pgpass")
  ] | length) == 1 and
  .secrets.product_health_reader_pgpass.file ==
    $product_health_pgpass_file and
  (.services.grafana.networks | has("production_database")) and
  ([.services | to_entries[] |
    select(.key != "grafana") |
    (.value.networks | has("production_database"))] | any | not) and
  .networks.production_api.external == true and
  .networks.production_api.name == "xe3-speakup-production-server-edge" and
  .networks.staging_api.external == true and
  .networks.staging_api.name == "xe3-speakup-staging_server_edge" and
  .networks.production_database.external == true and
  .networks.production_database.name == "xe3-speakup-production_database" and
  (.networks.monitor.external // false) == false and
  ([.services[] | .image | test("@sha256:[0-9a-f]{64}$")] | all)
' "$temporary_directory/compose.json" >/dev/null ||
  fail 'resolved observability Compose model violates its isolation contract'
grep -Fxq 'ExecStart=/usr/local/sbin/xe3-speakup-export-metrics --output /var/lib/xe3-speakup-observability/textfile/speakup.prom' \
  "$metrics_service" || fail 'systemd and node_exporter textfile paths are not one fixed contract'
! grep -Fq 'OBSERVABILITY_TEXTFILE_' "$metrics_environment_example" ||
  fail 'textfile path can still diverge through the metrics environment file'

readonly expected_log_set=$'/usr/local/nginx/logs/xe3-speakup-production-portal.access.log\n/usr/local/nginx/logs/xe3-speakup-production-portal.error.log\n/usr/local/nginx/logs/xe3-speakup-production-api.access.log\n/usr/local/nginx/logs/xe3-speakup-production-api.error.log\n/usr/local/nginx/logs/xe3-speakup-staging-portal.access.log\n/usr/local/nginx/logs/xe3-speakup-staging-portal.error.log\n/usr/local/nginx/logs/xe3-speakup-staging-api.access.log\n/usr/local/nginx/logs/xe3-speakup-staging-api.error.log\n/usr/local/nginx/logs/xe3-speakup-monitor.access.log\n/usr/local/nginx/logs/xe3-speakup-monitor.error.log'
actual_log_set=$(sed -nE 's|^(/usr/local/nginx/logs/[^[:space:]]+)([[:space:]]+\{)?$|\1|p' \
  "$logrotate_file")
[[ "$actual_log_set" == "$expected_log_set" ]] ||
  fail 'Nginx logrotate scope is not the exact ordered SpeakUp log set'
[[ $(grep -Ec '[[:space:]]+\{$' "$logrotate_file") -eq 1 ]] ||
  fail 'Nginx logrotate must open exactly one stanza after the final path'
! grep -Fq '*' "$logrotate_file" || fail 'Nginx logrotate must not contain globs'
grep -Fq 'kill -USR1' "$logrotate_file" ||
  fail 'Nginx logrotate does not reopen file handles'
[[ $(grep -Fc 'server_name monitor.speak-up.top;' "$monitor_nginx") -eq 2 ]] ||
  fail 'monitor Nginx host must have exact HTTP and HTTPS blocks'
grep -Fq 'proxy_pass http://127.0.0.1:13000;' "$monitor_nginx" ||
  fail 'monitor Nginx upstream is not loopback-only'
grep -Fq 'live/monitor.speak-up.top/fullchain.pem;' "$monitor_nginx" ||
  fail 'monitor Nginx does not use its independent certificate lineage'
grep -Fq 'live/monitor.speak-up.top/privkey.pem;' "$monitor_nginx" ||
  fail 'monitor Nginx does not use its independent private key lineage'
! grep -Fq 'live/speak-up.top/' "$monitor_nginx" ||
  fail 'monitor Nginx would mutate the exact Production certificate SAN contract'
grep -A2 -F 'location ^~ /metrics {' "$monitor_nginx" | grep -Fq 'return 404;' ||
  fail 'monitor Nginx does not deny the metrics path'
if grep -Eq 'proxy_pass http://0\.0\.0\.0|listen 13000' "$monitor_nginx"; then
  fail 'monitor Nginx exposed an internal listener'
fi

grep -Fq 'SpeakUpEndpointDown' "$rules_file" || fail 'endpoint alert is missing'
grep -Fq 'SpeakUpScrapeTargetMissing' "$rules_file" || fail 'absent scrape alert is missing'
grep -Fq 'SpeakUpEndpointProbeMissing' "$rules_file" || fail 'absent endpoint alert is missing'
grep -Fq 'SpeakUpHostMetricsMissing' "$rules_file" || fail 'absent textfile alert is missing'
grep -Fq 'speakup_host_filesystem_files_free' "$rules_file" ||
  fail 'host inode alerts do not use the bounded host exporter'
! grep -Fq 'node_filesystem_' "$rules_file" ||
  fail 'rules still depend on a node_exporter host-root mount'
grep -Fq 'SpeakUpCertificateExpiresWithin30Days' "$rules_file" ||
  fail '30-day certificate alert is missing'
grep -Fq 'SpeakUpCertificateExpiresWithin14Days' "$rules_file" ||
  fail '14-day certificate alert is missing'
grep -Fq 'SpeakUpCertificateExpiresWithin7Days' "$rules_file" ||
  fail '7-day certificate alert is missing'
grep -Fq 'SpeakUpTLSRenewalNotRunRecently' "$rules_file" ||
  fail 'twice-daily TLS renewal staleness alert is missing'
grep -Fq 'purpose="tls_renewal"' "$rules_file" ||
  fail 'TLS renewal staleness is not scoped to its closed purpose label'
grep -Fq '> 18 * 60 * 60' "$rules_file" ||
  fail 'TLS renewal staleness threshold is not explicit'
grep -Fq 'SpeakUpRestoreCheckNotRunMonthly' "$rules_file" ||
  fail 'monthly restore-check staleness alert is missing'
[[ $(grep -Fc 'purpose="postgres_restore_check"' "$rules_file") -eq 1 ]] ||
  fail 'PostgreSQL restore staleness is not a single closed purpose'
[[ $(grep -Fc 'purpose="portal_restore_check"' "$rules_file") -eq 1 ]] ||
  fail 'Portal restore staleness is not a single closed purpose'
[[ $(grep -Fc '> 31 * 24 * 60 * 60' "$rules_file") -eq 2 ]] ||
  fail 'monthly restore staleness thresholds are not explicit for both purposes'

jq --exit-status '
  .uid == "speakup-production-overview" and
  .editable == false and
  (.panels | length) >= 12 and
  ([.panels[].targets[]?.expr // ""] |
    any(contains("probe_success{job=\"blackbox-http\""))) and
  ([.panels[].targets[]?.expr // ""] |
    any(contains("probe_duration_seconds{job=\"blackbox-http\",environment=~\"$environment\"}"))) and
  ([.panels[].targets[]?.expr // ""] |
    any(contains("speakup_host_filesystem_files_free"))) and
  ([.panels[].targets[]?.expr // ""] |
    any(contains("speakup_provider_calls_total"))) and
  ([.panels[].targets[]?.expr // ""] |
    any(contains("speakup_provider_call_duration_seconds_bucket"))) and
  ([.panels[].targets[]?.expr // ""] |
    any(contains("speakup_provider_usage_tokens_total"))) and
  ([.panels[].targets[]?.expr // ""] |
    any(contains("speakup_http_server_request_duration_seconds_bucket")))
' "$dashboard_file" >/dev/null || fail 'Grafana dashboard is incomplete'

jq --exit-status '
  .uid == "speakup-product-health" and
  .title == "SpeakUp 产品健康" and
  (.description | contains("UTC")) and
  .timezone == "utc" and
  .editable == false and
  (.panels | length) >= 12 and
  ([.panels[].title] == [
    "每日练习用户",
    "终态练习结果",
    "练习完成率与提前结束率",
    "有效回答与重练回答",
    "重练占比：回答与练习",
    "确认回答的交互方式",
    "反馈与报告生成覆盖率",
    "评估任务：成功与失败",
    "评估终态成功率",
    "评估生命周期 P95 耗时",
    "证据不足与未知评估数量",
    "证据不足与未知评估占比"
  ]) and
  ([.panels[].description | length > 0] | all) and
  ([.panels[].targets[]? |
    .datasource.uid == "speakup-product-health-postgres" and
    .datasource.type == "grafana-postgresql-datasource"
  ] | all) and
  ([.panels[].targets[]?.rawSql // "" |
    (split("SELECT ") | length) ==
      (split("WHERE $__timeFilter(day_utc)") | length)
  ] | all) and
  ([.panels[].targets[]?.rawSql // "" |
    test("FROM public\\.product_health_daily_(practice_activity|session_outcomes|artifact_coverage|evaluation_health|scoreability)")
  ] | all) and
  ([.panels[].targets[]?.rawSql // "" |
    test("FROM public\\.(users|practice_sessions|practice_turns|evaluations|evaluation_feedback_items)")
  ] | any | not) and
  ([.panels[].targets[]?.rawSql // ""] |
    any(contains("active_practice_users"))) and
  ([.panels[].targets[]?.rawSql // ""] |
    any(contains("ready_coverage_rate"))) and
  ([.panels[].targets[]?.rawSql // ""] |
    any(contains("processing_lifecycle_p95_seconds"))) and
  ([.panels[].targets[]?.rawSql // ""] |
    any(contains("unknown_scoreability_evaluations")))
' "$product_health_dashboard" >/dev/null ||
  fail 'Product Health Grafana dashboard violates its anonymous SQL contract'

grep -Fxq '    uid: speakup-product-health-postgres' "$product_health_datasource" ||
  fail 'Product Health datasource UID is not fixed'
grep -Fxq '    url: postgres:5432' "$product_health_datasource" ||
  fail 'Product Health datasource does not use the private Postgres service'
grep -Fxq '    user: speakup_product_health_reader' "$product_health_datasource" ||
  fail 'Product Health datasource does not use its dedicated reader'
grep -Fq 'database: $__env{OBSERVABILITY_PRODUCT_HEALTH_DATABASE}' \
  "$product_health_datasource" || fail 'Product Health database is not provisioned explicitly'
grep -Fxq '      postgresVersion: 1000' "$product_health_datasource" ||
  fail 'Product Health datasource does not declare PostgreSQL 10+'
grep -Fxq '      maxOpenConns: 4' "$product_health_datasource" ||
  fail 'Product Health datasource connections are not bounded'
if grep -Eq '(^|[[:space:]])(password|secureJsonData):' "$product_health_datasource"; then
  fail 'Product Health datasource contains a credential field'
fi

# Private file validation is fail-closed and accounts for the runtime UIDs of
# standalone Compose bind mounts.
private_bin="$temporary_directory/private-bin"
mkdir -p "$private_bin"
cat >"$private_bin/stat" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
format=$2
path=$3
if [[ "${FAKE_BAD_PRIVATE_PATH:-}" == "$path" && "$format" == %a ]]; then
  printf '%s\n' 644
  exit
fi
case "$path" in
  */observability.env | */metrics.env)
    owner=0 mode=600
    ;;
  */alertmanager.yml)
    owner=65534 mode=400
    ;;
  */grafana-password)
    owner=472 mode=400
    ;;
  */product-health.pgpass)
    owner=472 mode=600
    ;;
  *) exit 2 ;;
esac
case "$format" in
  %u) printf '%s\n' "$owner" ;;
  %a) printf '%s\n' "$mode" ;;
  *) exit 2 ;;
esac
EOF
chmod 0755 "$private_bin/stat"
cp "$observability_directory/alertmanager.example.yml" "$temporary_directory/alertmanager.yml"
cat >"$temporary_directory/observability.env" <<EOF
OBSERVABILITY_ALERTMANAGER_CONFIG=$temporary_directory/alertmanager.yml
OBSERVABILITY_GRAFANA_HOST=monitor.speak-up.top
OBSERVABILITY_GRAFANA_ADMIN_USER=speakup-admin
OBSERVABILITY_GRAFANA_ADMIN_PASSWORD_FILE=$temporary_directory/grafana-password
OBSERVABILITY_PRODUCT_HEALTH_DATABASE=speakup
OBSERVABILITY_PRODUCT_HEALTH_PGPASS_FILE=$temporary_directory/product-health.pgpass
EOF
cat >"$temporary_directory/metrics.env" <<EOF
OBSERVABILITY_PRODUCTION_CERTIFICATE=$temporary_directory/production.pem
OBSERVABILITY_STAGING_CERTIFICATE=$temporary_directory/staging.pem
OBSERVABILITY_MONITOR_CERTIFICATE=$temporary_directory/monitor.pem
OBSERVABILITY_POSTGRES_BACKUP_ROOT=$temporary_directory/postgres-backups
OBSERVABILITY_PORTAL_BACKUP_ROOT=$temporary_directory/portal-backups
EOF
PATH="$private_bin:$PATH" "$private_file_validator" \
  --environment-file "$temporary_directory/observability.env" \
  --metrics-environment-file "$temporary_directory/metrics.env" >/dev/null
expect_failure 'public Grafana password' env \
  FAKE_BAD_PRIVATE_PATH="$temporary_directory/grafana-password" \
  PATH="$private_bin:$PATH" \
  "$private_file_validator" \
    --environment-file "$temporary_directory/observability.env" \
    --metrics-environment-file "$temporary_directory/metrics.env"
expect_failure 'public Product Health PGPASSFILE' env \
  FAKE_BAD_PRIVATE_PATH="$temporary_directory/product-health.pgpass" \
  PATH="$private_bin:$PATH" \
  "$private_file_validator" \
    --environment-file "$temporary_directory/observability.env" \
    --metrics-environment-file "$temporary_directory/metrics.env"

# The role configurator reads one scoped PGPASSFILE and sends the password only
# through psql stdin. Docker arguments and command output remain credential-free.
role_bin="$temporary_directory/role-bin"
role_args="$temporary_directory/role-args"
role_sql="$temporary_directory/role.sql"
mkdir -p "$role_bin"
cat >"$role_bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >"$FAKE_ROLE_ARGS"
cat >"$FAKE_ROLE_SQL"
printf '%s\n' 'BEGIN' 'ALTER ROLE' 'GRANT' 'COMMIT'
EOF
chmod 0755 "$role_bin/docker"
cat >"$temporary_directory/production.env" <<'EOF'
PRODUCTION_POSTGRES_DB=speakup
PRODUCTION_POSTGRES_USER=speakup
PRODUCTION_POSTGRES_PASSWORD=not-read-by-product-health-configurator
EOF
chmod 0600 "$temporary_directory/production.env" \
  "$temporary_directory/product-health.pgpass"
export FAKE_ROLE_ARGS="$role_args" FAKE_ROLE_SQL="$role_sql"
printf '%s\n' \
  'postgres:5432:another_database:speakup_product_health_reader:ProductHealthReader_1234567890abcdef' \
  >"$temporary_directory/wrong-scope-product-health.pgpass"
chmod 0600 "$temporary_directory/wrong-scope-product-health.pgpass"
expect_failure 'wrong Product Health database scope' env \
  PATH="$role_bin:$PATH" \
  "$product_health_reader" \
    --production-compose-file "$observability_directory/../production/compose.yaml" \
    --production-env-file "$temporary_directory/production.env" \
    --pgpass-file "$temporary_directory/wrong-scope-product-health.pgpass"
PATH="$role_bin:$PATH" "$product_health_reader" \
  --production-compose-file "$observability_directory/../production/compose.yaml" \
  --production-env-file "$temporary_directory/production.env" \
  --pgpass-file "$temporary_directory/product-health.pgpass" \
  >"$temporary_directory/role-output"
readonly product_health_fixture_password=ProductHealthReader_1234567890abcdef
! grep -Fq "$product_health_fixture_password" "$role_args" ||
  fail 'Product Health reader password leaked into Docker arguments'
! grep -Fq "$product_health_fixture_password" "$temporary_directory/role-output" ||
  fail 'Product Health reader password leaked into command output'
grep -Fq "ALTER ROLE speakup_product_health_reader WITH LOGIN PASSWORD '$product_health_fixture_password'" \
  "$role_sql" || fail 'Product Health reader password was not delivered over psql stdin'
grep -Fq 'REVOKE ALL ON ALL TABLES IN SCHEMA public FROM speakup_product_health_reader;' \
  "$role_sql" || fail 'Product Health reader does not revoke inherited table grants'
grep -Fq 'FROM pg_catalog.pg_auth_members AS membership' "$role_sql" ||
  fail 'Product Health reader does not reject role memberships'
[[ $(grep -Fc 'public.product_health_daily_' "$role_sql") -eq 5 ]] ||
  fail 'Product Health reader grant is not the exact five-view set'
! grep -Eq 'GRANT SELECT ON public\.(users|practice_sessions|practice_turns|evaluations)' \
  "$role_sql" || fail 'Product Health reader grants a raw business table'

# The off-host probe opens exactly one assigned incident, then comments and
# closes it on resolution using only GitHub's built-in token and Issues API.
probe_bin="$temporary_directory/probe-bin"
probe_state="$temporary_directory/probe.state"
probe_log="$temporary_directory/probe.log"
probe_summary="$temporary_directory/probe.summary"
mkdir -p "$probe_bin"
cat >"$probe_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FAKE_GH_LOG"
if [[ "$*" == *'--paginate --slurp'* ]]; then
  if [[ -s "$FAKE_GH_STATE" ]]; then
    title=$(<"$FAKE_GH_STATE")
    jq -n --arg title "$title" '[[{number: 42, title: $title, body: "<!-- speakup-production-probe-incident:v1 -->"}]]'
  else
    printf '%s\n' '[[]]'
  fi
  exit
fi
method=GET
path=''
previous=''
for argument in "$@"; do
  [[ "$previous" != --method ]] || method=$argument
  if [[ "$argument" == repos/* ]]; then path=$argument; fi
  previous=$argument
done
case "$method:$path" in
  POST:*/issues)
    payload=$(cat)
    jq -e '.assignees == ["Lq0412"] and (.body | contains("firing"))' \
      <<<"$payload" >/dev/null
    jq -r '.title' <<<"$payload" >"$FAKE_GH_STATE"
    printf '%s\n' 42
    ;;
  POST:*/comments)
    jq -e '.body | contains("resolved")' >/dev/null
    ;;
  PATCH:*/issues/42)
    jq -e '.state == "closed" and .state_reason == "completed"' >/dev/null
    rm -f "$FAKE_GH_STATE"
    ;;
  *) exit 2 ;;
esac
EOF
chmod 0755 "$probe_bin/gh"
export FAKE_GH_LOG="$probe_log" FAKE_GH_STATE="$probe_state"
export GITHUB_REPOSITORY=1024XEngineer/XE3-ESL
export GITHUB_SERVER_URL=https://github.com
export GITHUB_RUN_ID=123456 GITHUB_RUN_ATTEMPT=1 GITHUB_ACTOR=Lq0412
export GITHUB_STEP_SUMMARY="$probe_summary"
PATH="$probe_bin:$PATH" "$probe_incident" firing drill
PATH="$probe_bin:$PATH" "$probe_incident" firing drill
[[ $(grep -Fc -- '--method POST repos/1024XEngineer/XE3-ESL/issues ' "$probe_log") -eq 1 ]] ||
  fail 'repeated firing created duplicate GitHub incidents'
PATH="$probe_bin:$PATH" "$probe_incident" resolved drill
[[ ! -e "$probe_state" ]] || fail 'resolved drill incident remained open'
expect_failure 'resolve missing drill incident' env PATH="$probe_bin:$PATH" \
  "$probe_incident" resolved drill

: >"$probe_log"
PATH="$probe_bin:$PATH" "$probe_incident" failure production
grep -Fxq '[production-probe] SpeakUp public endpoint incident' "$probe_state" ||
  fail 'executed probe failure did not open the Production incident'
PATH="$probe_bin:$PATH" "$probe_incident" success production
[[ ! -e "$probe_state" ]] || fail 'executed probe success did not resolve the Production incident'
: >"$probe_log"
expect_failure 'skipped Production probe' env PATH="$probe_bin:$PATH" \
  "$probe_incident" skipped production
[[ ! -s "$probe_log" && ! -e "$probe_state" ]] ||
  fail 'skipped probe contacted GitHub or created a Production incident'

grep -Fq 'issues: write' "$probe_workflow" || fail 'off-host probe cannot write incident state'
grep -Fq 'continue-on-error: true' "$probe_workflow" ||
  fail 'off-host probe cannot notify before preserving failure status'
grep -Fq 'Preserve failing probe status' "$probe_workflow" ||
  fail 'off-host probe can silently pass after a failed public probe'
grep -Fq '"$PROBE_OUTCOME" production' "$probe_workflow" ||
  fail 'off-host probe does not classify the actual step outcome'
grep -Fq "steps.public-probe.outcome == 'failure'" "$probe_workflow" ||
  fail 'off-host workflow does not preserve only an executed probe failure'
! grep -Fq "steps.public-probe.outcome != 'success'" "$probe_workflow" ||
  fail 'off-host workflow still treats skipped probes as endpoint failures'
grep -Fq 'inputs:' "$probe_workflow" || fail 'off-host notification drill is not dispatchable'
[[ $(grep -Fc -- '--max-time 10' "$probe_workflow") -eq 2 ]] ||
  fail 'off-host retries can exhaust the job before incident recording'

printf '%s\n' 'Observability contract tests passed'
