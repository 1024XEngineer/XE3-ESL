# SpeakUp immutable Production contract

This directory defines the host-side Production topology for an already built
Release Candidate. It validates immutable inputs and renders Nginx, but it does
not deploy containers, run a migration, alter Nginx, or connect to a server.
Those state-changing steps remain unavailable until backup and rollback
orchestration is reviewed separately.

## Verified boundaries

- Compose project: `xe3-speakup-production`.
- Portal loopback: `127.0.0.1:18082`.
- Server loopback: `127.0.0.1:18083`.
- Portal data: existing external volume `xe3-speakup-portal-data`.
- PostgreSQL data: explicitly provisioned external volume
  `xe3-speakup-postgres-data`.
- Server edge: pre-provisioned external network
  `xe3-speakup-production-server-edge`; its exact gateway `/32` is the only
  trusted reverse-proxy peer.
- Portal and Server images: exact repositories and `sha256` digests from
  `release-manifest.json`; there is no `build` or mutable image tag.
- PostgreSQL has no host port. Its network is internal to this Compose project.
- Nginx is the only public HTTP/TLS entry point; `/metrics` is not public.

The external Portal volume name is the currently verified Production data
volume. If it is missing, stop and recover the existing volume; do **not** create
an empty volume with the same name. The PostgreSQL volume is also external so a
missing volume cannot silently initialize an empty Production database during a
release. Its one-time creation belongs to the audited server bootstrap.

## Prerequisites

- Bash, `jq`, `curl`, Docker Engine, and Docker Compose.
- The two external volumes above already exist on the intended host.
- The external Server edge network exists with an audited, non-conflicting
  subnet and fixed gateway.
- A non-empty Server environment file outside the repository.
- The existing `speak-up.top` certificate lineage safely expanded to the exact
  Portal, redirect, and API SAN set through [`deploy/tls/`](../tls/README.md).
- A populated ACME web root.
- Registry read access for the manifest's Portal and Server image digests.
- A real, current-UID-owned `PRODUCTION_PUBLIC_ROOT` that is not group- or
  world-writable.

Do not copy local `.env` defaults into Production and do not commit any filled
environment file. The Server environment file contains real provider
configuration; Compose owns `DATABASE_URL`, `SERVER_HOST`, and `SERVER_PORT`.

## Prepare configuration

Copy `production.env.example` outside the repository and populate every value:

```sh
install -d -m 0750 /etc/speakup
install -m 0600 deploy/production/production.env.example \
  /etc/speakup/production.env
install -m 0600 /secure/source/production-server.env \
  /etc/speakup/production-server.env
install -d -m 0755 /var/www/speakup-production-public
```

Preserve the existing Production Certbot configuration and webroot. The TLS
lifecycle contract adds only an `api.speak-up.top` bootstrap vhost, expands the
existing `speak-up.top` lineage with the same webroot, and validates the exact
three-name certificate before this deployment template may use it. The
certificate paths here must stay aligned with `TLS_CERTBOT_CONFIG_ROOT`.
Keep the private-key setting on Certbot's stable
`live/speak-up.top/privkey.pem` symbolic link: validation resolves the current
archive target and requires that target to be a non-empty regular file owned by
the invoking user with mode `0400` or `0600`. Renewal therefore does not require
an environment-file edit. Other secret files remain regular-file-only and may
not be symbolic links.

`PRODUCTION_POSTGRES_PASSWORD` is restricted to at least 24 URL-safe
characters because it is inserted into a PostgreSQL URL. `PORTAL_ADMIN_PASSWORD`
must contain at least 16 characters. Hostnames and file paths are validated and
unknown configuration keys are rejected.

Set `PRODUCTION_SERVER_EDGE_GATEWAY_CIDR` to the external network's exact IPv4
gateway followed by `/32`. For example, if `docker network inspect` reports
`172.31.253.1`, configure `172.31.253.1/32`. Validation compares this value to
the live network; broad RFC1918 ranges are rejected. A host Nginx request sent
through a loopback-published Docker port reaches the container from that bridge
gateway, so the Server trusts only this one peer when reading
`X-Forwarded-For`.

## Validate without changing Production

Acquire the exact manifest produced by the selected successful Release
Candidate, then run:

```sh
./deploy/production/manage.sh validate \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --env-file /etc/speakup/production.env
```

Validation checks configuration, manifest repositories and digests, the
resolved Compose model, both external volumes, the external Server edge network,
and the rendered Nginx template. It does not pull images or create/update
containers or networks.

## Render Nginx for review

```sh
./deploy/production/manage.sh render-nginx \
  --env-file /etc/speakup/production.env \
  --output /opt/speakup/releases/v0.1.1/production-nginx.conf
```

Rendering only writes the requested file. It never installs or reloads Nginx.
The template preserves Portal request limits, canonical-host redirect, API
Bearer authentication, WebSocket upgrade headers, ACME challenges, and exact
`/metrics` denial. It serves only the strict versioned Android APK, checksum,
and metadata routes from `PRODUCTION_PUBLIC_ROOT`; current metadata is not
cached, versioned files are immutable, and unknown or directory paths return
`404`. The API host returns `404` for the complete Android download namespace.

Nginx rendering does not publish or activate an APK. Build, validate, publish,
activate, and roll back that separate state with the
[Android publication contract](../android-download/README.md). Publish a new
version without activation first; switch the current metadata only after the
Production smoke checks pass.

Install this vhost in place of the legacy Portal vhost; do not load both at the
same time. The rate-limit zones and public hostnames intentionally have one
owner. The relative `logs/` paths preserve the existing `/usr/local/nginx`
prefix used by the Production host.

## Read-only runtime checks

After a later audited deployment has installed this stack:

```sh
./deploy/production/manage.sh status \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --env-file /etc/speakup/production.env

./deploy/production/manage.sh verify \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --env-file /etc/speakup/production.env
```

`verify` checks Portal `/`, Server `/health`, and Server `/readyz` over the
loopback bindings. Public HTTPS and business-route smoke tests belong to the
release smoke contract. There is intentionally no `deploy` or `down` command in
this directory yet.

## PostgreSQL logical backup and isolated restore check

The Production PostgreSQL data volume has a separate, fail-closed logical
backup contract. The backup script discovers the running Production PostgreSQL
container through its fixed Compose labels, verifies its image, database,
healthy state, source volume, and clean migration version, then creates a
custom-format `pg_dump`. The dump waits at most 30 seconds for any table lock
instead of blocking indefinitely. Both `backup daily` and `backup predeploy`
write the dump, checksum, and release-linked JSON metadata under a private
`.partial-*` directory, then complete an isolated `pg_restore`. Only after that
restore succeeds is the directory published atomically as a finalized backup
and expired finalized backups pruned. A failed dump, checksum, or restore is
never published and never triggers retention deletion. The current run's exact
partial directory is removed on failure using only the three fixed contract
file names; unexpected entries stop cleanup instead of allowing recursive
deletion.

The daily timer therefore creates and restore-verifies a new backup on every
successful run. The separate `check [BACKUP_ID]` command revalidates the latest
or named finalized backup later: it verifies age, metadata, and checksum, then
restores with the exact digest-pinned PostgreSQL image recorded by that backup
into a temporary Docker volume with networking disabled. Historical backup
checks therefore require their recorded image to remain available locally;
they do not silently substitute the current Production image. Neither path
mounts or changes the Production PostgreSQL volume. Backup and restore-check
processes share
`/run/lock/xe3-postgres-backup.lock`, so they fail instead of overlapping.

Install the script, private configuration, and systemd units on the Production
host:

```sh
install -d -m 0750 /etc/speakup
install -m 0755 deploy/production/xe3-postgres-backup \
  /usr/local/sbin/xe3-postgres-backup
install -m 0600 deploy/production/postgres-backup.env.example \
  /etc/speakup/postgres-backup.env
install -m 0644 deploy/production/xe3-postgres-backup.service \
  deploy/production/xe3-postgres-backup.timer \
  deploy/production/xe3-postgres-restore-check.service \
  /etc/systemd/system/
```

Populate every value in `/etc/speakup/postgres-backup.env` before starting a
unit. `POSTGRES_BACKUP_IMAGE` must be the exact repository digest used by the
Production Compose contract; tag-only references and bare image IDs are
rejected.
The database, user, and source volume must match the running Production
PostgreSQL service. Set `POSTGRES_BACKUP_DEPLOYMENT_VERSION` and
`POSTGRES_BACKUP_GIT_SHA` from the deployed, reviewed release manifest and
update both when Production is promoted. Choose explicit retention and maximum
backup age values for the approved recovery policy; the script has no silent
defaults. The file contains no database password because backup access stays
inside the already-running PostgreSQL container through the local Docker Unix
socket.

Enable the daily backup only after a manual backup and isolated restore check
both succeed:

```sh
systemctl daemon-reload
systemctl start xe3-postgres-backup.service
systemctl start xe3-postgres-restore-check.service
systemctl enable --now xe3-postgres-backup.timer
systemctl list-timers xe3-postgres-backup.timer
```

The backup unit runs `xe3-postgres-backup backup daily`; the restore-check unit
runs `xe3-postgres-backup check`, which selects the latest finalized backup when
no ID is given. `StateDirectory=speakup/postgres-backups` must create
`/var/lib/speakup/postgres-backups` before the script starts, owned by the
service user with mode `0700`; the script rejects a missing, symlinked,
wrong-owner, or wrong-mode root instead of creating or repairing it. Both units
use a `0077` umask and a two-hour start timeout. Their process network namespace
is private and only `AF_UNIX` is permitted so the Docker Unix socket remains
usable without granting the unit IP network access.

The configured application database user is also the owner of objects created
by the migration command. Dumps and isolated restores both use
`--no-owner --no-privileges`: they preserve the application schema and data
under that same database user without requiring cluster roles, ownership
reassignment, or grant restoration. This is a deliberate single-application
database boundary, not a backup of PostgreSQL cluster identities.

Treat a non-zero result from either service, an overdue timer, a failed restore
check, or insufficient space below `/var/lib/speakup/postgres-backups` as an
alert. The exact evidence is available without exposing configuration values:

```sh
systemctl status xe3-postgres-backup.service \
  xe3-postgres-restore-check.service xe3-postgres-backup.timer
journalctl --unit xe3-postgres-backup.service \
  --unit xe3-postgres-restore-check.service
```

Wire those systemd failure states and filesystem-capacity signals into the
host's monitoring before relying on the timer. Run the isolated restore check
after any image or PostgreSQL major-version change and as part of the audited
pre-deployment gate. The daily service already proves its newly created backup;
the later check proves that a selected finalized backup still passes the same
restore contract. A backup that has not restored successfully is not valid
release evidence.

This contract deliberately provides **no Production restore command**. It also
does not capture PostgreSQL roles or tablespaces and does not configure WAL
archiving or point-in-time recovery (PITR). Restoring data can discard writes
made after the selected backup, so an actual Production restore requires a
separate reviewed disaster-recovery runbook, an explicit outage, a named backup
ID, and operator approval. Image rollback must never silently restore this
database.

## Reproducible checks

```sh
make check-production-backup
make check-android-download
make check-production-deploy
make check-production-nginx
```

The tests resolve the Compose model and prove digest-only images, fixed project
name, loopback ports, internal database network, external volume names, Nginx
headers, and fail-closed validation without contacting Production.

## References

- [Docker Compose services](https://docs.docker.com/reference/compose-file/services/)
- [Docker Compose external volumes](https://docs.docker.com/reference/compose-file/volumes/)
- [Docker Compose project names](https://docs.docker.com/compose/how-tos/project-name/)
- [Docker Compose startup order](https://docs.docker.com/compose/how-tos/startup-order/)
- [Nginx proxy module](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)
- [PostgreSQL SQL dump backup](https://www.postgresql.org/docs/18/backup-dump.html)
- [PostgreSQL `pg_restore`](https://www.postgresql.org/docs/18/app-pgrestore.html)
