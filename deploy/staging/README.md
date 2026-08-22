# SpeakUp immutable Staging stack

This directory is the host-side contract for running a Release Candidate in an
isolated Staging environment. It does not connect to a server, install Nginx,
issue a certificate, or change Production by itself.

## Verified inputs and design choices

The Release Candidate workflow emits `release-manifest.json` with the exact
Portal and Server repositories and their `sha256` digests. The Server image
contains both `/usr/local/bin/speakup-server` and the explicit
`/usr/local/bin/speakup-migrate` command, and exposes `/health` and `/readyz`.
PostgreSQL 18 stores its versioned data below `/var/lib/postgresql`, so the
named volume is mounted at that parent path.

This Staging contract deliberately uses:

- Compose project `xe3-speakup-staging`, never the Production project name;
- loopback ports `28082` for Portal and `28083` for Server;
- project-scoped Portal and PostgreSQL volumes;
- separate Portal, Server-egress, and internal database networks;
- image references of the form `repository@sha256:digest`, with no `build`;
- a one-shot migration before either application container is updated;
- HTTP Basic Authentication on the Staging Portal only;
- TLS plus the application's Bearer authentication on the Staging API, so the
  mobile client's `Authorization` header is never consumed by Nginx;
- an exact public `/metrics` denial, while internal metrics work remains in
  its own Issue.

The selected hostnames are `staging.speak-up.top` for Portal and
`staging-api.speak-up.top` for API; the latter matches the Staging APK's fixed
API base URL. Their DNS targets, TLS paths, htpasswd path, provider credentials,
and server environment are externally injected and are not committed.

## Prerequisites

- Bash, `jq`, `curl`, Docker Engine, and Docker Compose with `up --wait` support.
- Nginx with the HTTP SSL, proxy, and Basic Auth modules.
- A certificate whose SANs cover both Staging hostnames.
- A populated htpasswd file and a real Staging Server environment file.
- Registry credentials with read access to the two GHCR images.
- A real, current-UID-owned `STAGING_PUBLIC_ROOT` that is not group- or
  world-writable.

The Server environment file must contain the real Staging provider and secret
configuration required by the application. Use the repository `.env.example`
only as a key reference; do not copy local defaults as deployment values and do
not disable OSS or OCR to bypass missing configuration. It must not define
`DATABASE_URL`, `SERVER_HOST`, or `SERVER_PORT`; Compose owns those three values.

## 1. Acquire a manifest and configuration

Download the manifest artifact from the selected, successful Release Candidate
run. For example:

```sh
gh run download RUN_ID \
  --repo 1024XEngineer/XE3-ESL \
  --name speakup-v0.1.1-release-manifest \
  --dir /opt/speakup/releases/v0.1.1
```

Copy `staging.env.example` to a location outside the repository, populate every
blank value, and restrict both environment files:

```sh
install -d -m 0750 /etc/speakup
install -m 0600 staging.env.example /etc/speakup/staging.env
install -m 0600 /secure/source/staging-server.env \
  /etc/speakup/staging-server.env
install -d -m 0755 /var/www/speakup-staging-public
```

Before public verification, create A records for both selected hostnames (and
AAAA records only when the host is intentionally IPv6-reachable) and point them
to the Staging host. IP addresses stay in the DNS/operations system, not this
repository.

`STAGING_POSTGRES_PASSWORD` accepts URL-safe characters and must contain at
least 24 characters. A suitable value can be generated with
`openssl rand -hex 32`. Create the Portal access file with the host's `htpasswd`
tool; neither plaintext credentials nor the generated file belong in the
repository. Do not add HTTP Basic Auth to the API host: the Staging APK uses its
`Authorization: Bearer` header for application identity.

## 2. Validate without changing containers

```sh
./deploy/staging/manage.sh validate \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --env-file /etc/speakup/staging.env
```

Validation fails on a missing or malformed manifest, a mutable or placeholder
image reference, a missing configuration value, an empty Server environment
file, or an invalid Compose model. It does not pull images or contact the
application providers.

## 3. Deploy the immutable Staging images

```sh
./deploy/staging/manage.sh deploy \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --env-file /etc/speakup/staging.env
```

The command performs the following fail-closed sequence:

1. validate the manifest, external configuration, Compose model, and Nginx
   template;
2. pull the pinned PostgreSQL, Portal, and Server images;
3. start only PostgreSQL and wait for its health check;
4. run `/usr/local/bin/speakup-migrate up` in a one-shot container;
5. only after migration succeeds, update Portal and Server and wait for their
   container health checks;
6. verify Portal `/`, Server `/health`, and Server `/readyz` over loopback.

If migration fails, the command exits before the Portal or Server update. A
failed application health check is also an explicit deployment failure; this
Issue does not add Production promotion or automatic rollback behavior.

## 4. Render and install the Nginx configuration

Rendering writes only the requested output file. It never reloads Nginx:

```sh
./deploy/staging/manage.sh render-nginx \
  --env-file /etc/speakup/staging.env \
  --output /opt/speakup/releases/v0.1.1/staging-nginx.conf
```

Review the rendered file, install it at the host's configured include path,
then test and reload Nginx using the paths appropriate to that host. The
committed template keeps ACME HTTP challenges reachable, redirects other HTTP
traffic to HTTPS, protects the Portal with Basic Auth, preserves the API Bearer
header and WebSocket upgrades, and returns `404` for exact `/metrics` requests
before they reach either application. It also serves only the strict versioned
Android APK, checksum, and metadata routes from `STAGING_PUBLIC_ROOT`; the
current metadata is not cached, versioned files are immutable, and unknown or
directory paths return `404`. These Portal download routes inherit Basic Auth.
The API host intentionally has no `auth_basic` directive and returns `404` for
the complete Android download namespace.

Build, validate, publish, activate, and roll back the public APK bundle with
the separate [Android publication contract](../android-download/README.md).

## 5. Verify

```sh
./deploy/staging/manage.sh status \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --env-file /etc/speakup/staging.env

./deploy/staging/manage.sh verify \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --env-file /etc/speakup/staging.env

curl --fail --user staging-user https://staging.speak-up.top/
curl --fail --user staging-user \
  https://staging.speak-up.top/downloads/android/release.json
curl --fail https://staging-api.speak-up.top/health
curl --fail https://staging-api.speak-up.top/readyz
test "$(curl --silent --output /dev/null --write-out '%{http_code}' \
  https://staging-api.speak-up.top/metrics)" = 404
```

The Portal command prompts for the Basic Auth password instead of placing it in
the command. `/health` and `/readyz` stay directly verifiable over API HTTPS;
business routes continue to enforce the application's Bearer authentication.
Public HTTPS checks require DNS, certificate, and firewall setup that remain
external to this repository.

## 6. Stop or clean up Staging

```sh
./deploy/staging/manage.sh down \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --env-file /etc/speakup/staging.env
```

`down` removes only the `xe3-speakup-staging` containers and networks and keeps
`xe3-speakup-staging_portal_data` and
`xe3-speakup-staging_postgres_data`. Remove the rendered Nginx include and
reload Nginx if the public Staging entry points should also disappear. Volume
deletion is intentionally not automated; inspect and back up these explicitly
Staging-scoped volumes before deleting them by name.

## Reproducible contract checks

```sh
make check-android-download
make check-staging-deploy
make check-staging-nginx
```

The first command checks manifest and environment failure paths, the resolved
Compose isolation model, endpoint verification, and the rule that a migration
failure cannot switch applications. The second renders a temporary TLS config
and runs `nginx -t` in a pinned Docker Official Nginx image.

## Official references

- [Docker Compose image and healthcheck reference](https://docs.docker.com/reference/compose-file/services/)
- [Docker Compose startup order](https://docs.docker.com/compose/how-tos/startup-order/)
- [Docker Compose project isolation](https://docs.docker.com/compose/how-tos/project-name/)
- [Docker Official PostgreSQL image storage contract](https://github.com/docker-library/docs/blob/master/postgres/content.md)
- [Nginx proxy module](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)
- [Nginx TLS module](https://nginx.org/en/docs/http/ngx_http_ssl_module.html)
- [Nginx Basic Auth module](https://nginx.org/en/docs/http/ngx_http_auth_basic_module.html)
- [GitHub Actions artifact download](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/download-workflow-artifacts)
