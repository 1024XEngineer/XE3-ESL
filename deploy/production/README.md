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
`/metrics` denial. APK static delivery is intentionally absent until its own
versioned publication contract is reviewed.

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

## Reproducible checks

```sh
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
