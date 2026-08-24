# SpeakUp observability

This directory defines the isolated production monitoring boundary for SpeakUp.
It does not expose Prometheus, Alertmanager, node_exporter, blackbox_exporter, or
application `/metrics` ports to the public network. Only Grafana is published on
the host loopback interface; Nginx may expose it as `monitor.speak-up.top` after
DNS and certificate validation.

The shared server remains protected by three scope rules:

- no Docker socket is mounted into an observability container;
- host container metrics are exported by a fixed script that inspects only the
  `xe3-speakup-production` and `xe3-speakup-staging` Compose projects;
- log rotation lists exact SpeakUp files and never rotates another team's logs.

## Components

- Prometheus: 30-day/8-GB bounded retention.
- Alertmanager: private SMTP configuration supplied by the server operator.
- Grafana: provisioned Prometheus and restricted PostgreSQL data sources, with
  separate system and Product Health dashboards. New users default to the
  Simplified Chinese (`zh-Hans`) interface.
- blackbox_exporter: Portal, API, and Android release HTTPS probes.
- node_exporter: only the bounded SpeakUp textfile collector. It has no host
  root mount; the reviewed host-side exporter emits root disk and inode values.
- GitHub Actions `Production probe`: an off-host five-minute public endpoint
  check so a full server outage remains visible when the local stack is down.

The provisioned dashboard keeps Portal, API, Android metadata, container,
disk/inode, API, and provider status separate. Public probes expose both
`probe_success` and per-target `probe_duration_seconds`. Safety-unit alerts are
also time-bounded: the twice-daily TLS renewal becomes stale after 18 hours,
while each explicitly named PostgreSQL or Portal restore check becomes stale
after 31 days even when its previous run succeeded.

Safety-check success time comes only from the mtime of three fixed markers under
`/var/lib/speakup/safety-checks`; the exporter does not parse journals or depend
on systemd's volatile exit timestamps. The directory must be `root:root` mode
`0700`, and each empty marker must be `root:root` mode `0600`. A missing marker,
non-regular file, ownership or mode mismatch, or future mtime exports both
success and timestamp as zero for that check.

All container images and runtime limits are explicit. Grafana is the only
service with a published port, and that port is `127.0.0.1:13000`.

## Product Health boundary

Migration `000008_product_health_views` creates five daily aggregate views in
the application schema. They expose only UTC buckets, bounded enum dimensions,
counts, rates, and latency aggregates. They never expose a user, Session, Turn,
Evaluation, transcript, answer, report body, or other identifier/content field.
The migration revokes `PUBLIC` access to every view.

`configure-product-health-reader` provisions the login role
`speakup_product_health_reader` after the migration is applied. It first revokes
all table access, then grants `SELECT` on exactly those five views. The role is
non-inheriting, read-only by default, connection-limited, and time-bounded.
Grafana uses that role only through the existing internal
`xe3-speakup-production_database` Docker network. PostgreSQL receives no host
or public port.

This follows Grafana's recommendation to use a dedicated database user with
only `SELECT` on the necessary views and PostgreSQL's separate `USAGE`/`SELECT`
privilege model. The datasource supplies no password field: Grafana reads the
official `PGPASSFILE` instead.

The first dashboard intentionally reports only facts the current schema can
prove:

- active practice users had at least one confirmed answer that UTC day;
- completion and early-end rates use only terminal Sessions as the denominator;
- Retry has both Turn share and Session share;
- `TEXT`, `PUSH_TO_TALK`, and unknown interaction modes remain separate;
- Turn Feedback eligibility requires the frozen Session policy to say
  `speech_feedback_allowed=true`; missing/malformed policy remains unknown;
- Feedback/Report coverage means scheduled or READY generation, never viewing;
- Evaluation status and latency are grouped by the job's UTC creation day, so
  a recent bucket can change as its queued jobs reach a terminal state;
- initial queue latency is `started_at-created_at`; after-first-start and total
  lifecycle include retry/dependency waits because no attempt-history table
  exists, so the dashboard does not call either value pure processing time;
- scoreability keeps `PROVISIONAL`, `INSUFFICIENT`, and unknown separate.

Rates use `NULL` for a zero denominator, and migrations do not generate empty
calendar rows. Account deletion cascades through the source facts, so this is a
current operational health view rather than an immutable historical warehouse.
The deterministic migration integration fixture is the canonical
Staging-compatible seed for reproducing and checking every first-version
formula; it creates only an isolated test schema and is never loaded into
Production.

## Application metrics boundary

The production and Staging Server Compose definitions set `METRICS_HOST=0.0.0.0`
inside their private Docker networks and give the services unique network
aliases. There is still no host or public port for `9090`; Prometheus reaches it
only through the existing SpeakUp networks. Nginx continues to return `404` for
public `/metrics` requests.

The exported HTTP series are bounded by method, route template, status class,
and environment. Provider series must use only fixed `provider`, `capability`,
`outcome`, `error_kind`, and `unit` labels. Never use user IDs, request IDs,
models supplied by a caller, raw errors, text, audio, object keys, credentials,
or session tokens as metric labels.

## Private files

Create these files on the server; never commit or paste their populated values:

```text
/etc/speakup/observability.env
/etc/speakup/observability-metrics.env
/etc/speakup/alertmanager.yml
/etc/speakup/grafana-admin-password
/etc/speakup/product-health-reader.pgpass
```

Start from `observability.env.example`, `observability-metrics.env.example`, and
`alertmanager.example.yml`. Standalone Compose bind mounts preserve host
ownership. Install `observability.env` and `observability-metrics.env` as
`root:root` mode `0600`, Alertmanager configuration as UID `65534` mode `0400`,
and the one-line newline-terminated Grafana password as UID `472` mode `0400`.
Create the Product Health password file as UID `472` mode `0600`, with exactly
one scoped entry and a fresh 32-128 character base64url password:

```text
postgres:5432:speakup:speakup_product_health_reader:REPLACE_WITH_RANDOM_BASE64URL
```

For the initial installation, this root-only sequence creates the file without
printing the credential or putting it in a command argument. It refuses to
replace an existing credential:

```bash
set -euo pipefail
install -d -o root -g root -m 0750 /etc/speakup
test ! -e /etc/speakup/product-health-reader.pgpass
product_health_pgpass_tmp=$(mktemp /etc/speakup/.product-health-reader.pgpass.XXXXXX)
trap 'rm -f -- "$product_health_pgpass_tmp"' EXIT
{
  printf 'postgres:5432:speakup:speakup_product_health_reader:'
  openssl rand -hex 32
} >"$product_health_pgpass_tmp"
chown 472:472 "$product_health_pgpass_tmp"
chmod 0600 "$product_health_pgpass_tmp"
ln "$product_health_pgpass_tmp" /etc/speakup/product-health-reader.pgpass
rm -f -- "$product_health_pgpass_tmp"
trap - EXIT
```

The service UIDs match the pinned container images without making a secret
group- or world-readable. Validate all five files before Compose reads them:

```bash
/usr/local/sbin/xe3-speakup-observability-validate-private-files \
  --environment-file /etc/speakup/observability.env \
  --metrics-environment-file /etc/speakup/observability-metrics.env
```

## DNS and TLS prerequisite

Do not install `monitor-nginx.conf` until both conditions are true:

1. `monitor.speak-up.top` resolves to the production server;
2. the independent `monitor.speak-up.top` certificate lineage has exactly that
   single SAN and has been activated after `nginx -t`.

The monitor lineage reuses the reviewed Production ACME account and HTTP-01
webroot but never changes the exact three-SAN `speak-up.top` Production lineage.
Issue, verify, and activate it through `deploy/tls/manage.sh`:

```bash
/usr/local/sbin/xe3-speakup-tls issue-monitor --env-file /etc/speakup/tls.env
/usr/local/sbin/xe3-speakup-tls verify --environment monitor --env-file /etc/speakup/tls.env
/usr/local/sbin/xe3-speakup-tls activate --environment monitor --env-file /etc/speakup/tls.env
```

## Installation order

Keep every reviewed observability source under
`/opt/xe3-speakup-observability/releases/<full-git-sha>` and make
`/opt/xe3-speakup-observability/source` a symlink to the selected release.
Never overwrite a release directory; record the previous symlink target before
switching it so rollback selects an exact source. Then:

1. Install the private files above, install `validate-private-files` as
   `/usr/local/sbin/xe3-speakup-observability-validate-private-files`, and run
   the validator.
2. Apply Production database migrations through the reviewed release contract.
   Install `configure-product-health-reader` as
   `/usr/local/sbin/xe3-speakup-configure-product-health-reader`, then provision
   or rotate the reader without placing its password in arguments or logs:

   ```bash
   install -o root -g root -m 0755 \
     /opt/xe3-speakup-observability/source/configure-product-health-reader \
     /usr/local/sbin/xe3-speakup-configure-product-health-reader
   test -f /opt/xe3-speakup-production/source/deploy/production/compose.yaml
   /usr/local/sbin/xe3-speakup-configure-product-health-reader \
     --production-compose-file /opt/xe3-speakup-production/source/deploy/production/compose.yaml \
     --production-env-file /etc/speakup/production.env \
     --pgpass-file /etc/speakup/product-health-reader.pgpass
   ```

3. Install the current PostgreSQL restore-check, Portal SQLite restore-check,
   and TLS renewal units from their deployment directories. Reload systemd and
   successfully run each check once so systemd creates its persistent marker.
4. Install `xe3-speakup-export-metrics` as
   `/usr/local/sbin/xe3-speakup-export-metrics` with mode `0755`.
5. Install the metrics service/timer under `/etc/systemd/system`, reload systemd,
   and start the timer.
6. Install `xe3-speakup-nginx.logrotate` as
   `/etc/logrotate.d/xe3-speakup-nginx` with mode `0644`.
7. Recreate only the SpeakUp Staging and Production containers so their
   per-container `json-file` limits and internal metrics listener take effect.
8. Start the monitoring stack with the private environment file:

   ```bash
   docker compose \
     --env-file /etc/speakup/observability.env \
     --file /opt/xe3-speakup-observability/source/compose.yaml \
     up --detach --pull always --wait
   ```

9. Confirm every Prometheus target is up and the Product Health datasource can
   query only its five views from a loopback Grafana session.
10. After DNS/TLS validation, install `monitor-nginx.conf`, run
   `/usr/local/nginx/sbin/nginx -t`, and reload only after it succeeds.

For an existing installation, reinstall all three producer units before the
new exporter, run `systemctl daemon-reload`, then start each check manually.
Previous journal or unit timestamps are intentionally not migrated or inferred;
until a check completes successfully, its metrics remain `0/0`. Do not create
or touch a marker by hand. The `StateDirectory=` contract keeps valid markers
across later daemon reloads and host reboots.

Do not use `docker compose down --volumes`; the three named observability volumes
contain monitoring history, Alertmanager state, and Grafana configuration.

## Verification

Run repository contracts before touching the server:

```bash
make check-observability
make check-staging-deploy
make check-production-deploy
```

Then verify the installed runtime:

```bash
systemctl status xe3-speakup-observability-metrics.timer
systemctl status xe3-speakup-observability-metrics.service
docker compose \
  --env-file /etc/speakup/observability.env \
  --file /opt/xe3-speakup-observability/source/compose.yaml \
  ps
curl --fail http://127.0.0.1:13000/api/health
curl --fail --location https://monitor.speak-up.top/api/health
```

Public `/metrics` must still return `404` on Portal, API, and monitor hosts.

## Safe off-host alert drill

The `Production probe` workflow provides two manual drill modes and does not
stop, restart, or alter a production service:

1. Dispatch `drill=firing`. The workflow intentionally finishes red after it
   opens and assigns one `[production-probe drill]` GitHub Issue.
2. Confirm the assignee received GitHub's native notification and record the
   Issue and workflow-run URLs.
3. Dispatch `drill=resolved`. The workflow comments with the resolved run and
   closes the same Issue.

The Issue history is the sanitized firing/resolved receipt and uses only the
built-in `GITHUB_TOKEN`; no notification secret is required. Scheduled real
probe failures use a separate Issue and resolve it after a successful run.
Never make the Portal, API, or APK unavailable to exercise this path. The local
Alertmanager receiver must be tested separately after its private SMTP settings
are installed; do not commit those settings.

## Rollback

- If Product Health provisioning fails before Grafana is recreated, do not
  start the new Grafana model. Correct the private file or database migration
  first; none of the new database or monitoring ports is public.
- To restore Grafana availability, select the recorded preceding directory
  under `/opt/xe3-speakup-observability/releases`, atomically restore the
  `/opt/xe3-speakup-observability/source` symlink to it, validate its private
  files, and recreate only Grafana:

  ```bash
  /usr/local/sbin/xe3-speakup-observability-validate-private-files \
    --environment-file /etc/speakup/observability.env \
    --metrics-environment-file /etc/speakup/observability-metrics.env
  docker compose \
    --project-name xe3-speakup-observability \
    --env-file /etc/speakup/observability.env \
    --file /opt/xe3-speakup-observability/source/compose.yaml \
    up --detach --no-deps --force-recreate --wait grafana
  curl --fail http://127.0.0.1:13000/api/health
  ```

  Keep the Product Health role, views, and root-private PGPASSFILE in place.
  They are additive and cannot read raw tables; Production rollback never runs
  a down migration. Removing them belongs to a separately reviewed database
  change, not an availability rollback.
- If the stack fails, keep its volumes and stop only the
  `xe3-speakup-observability` Compose project.
- Restore the previous SpeakUp Compose files and recreate only those services if
  the metrics-network or logging change is the cause.
- Remove only the monitor Nginx vhost after `nginx -t` verifies the remaining
  configuration.
- Removing observability must not remove Production/Staging networks, databases,
  Portal data, certificates, or another team's containers.
