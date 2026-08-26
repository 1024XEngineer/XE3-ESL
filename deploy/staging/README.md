# SpeakUp immutable Staging stack

This directory is the host-side contract for running a Release Candidate in an
isolated Staging environment. It does not connect to a server, install Nginx,
issue a certificate, or change Production by itself.

Runtime deployment and edge rendering deliberately use separate configuration
files and commands. This removes the runtime command's need to read TLS,
htpasswd, ACME, or public-root inputs; it is a functional dependency boundary,
not host-enforced privilege isolation.

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
- a runtime-only configuration for PostgreSQL, Portal, and Server containers;
- an edge-only configuration for TLS, Basic Auth, ACME, and public downloads;
- HTTP Basic Authentication on the Staging Portal only;
- TLS plus the application's Bearer authentication on the Staging API, so the
  mobile client's `Authorization` header is never consumed by Nginx;
- an exact public `/metrics` denial, while internal metrics work remains in
  its own Issue.

The hostnames `staging.speak-up.top` for Portal and
`staging-api.speak-up.top` for API are fixed repository contracts; the latter
matches the Staging APK's fixed API base URL. They are not repeated in either
environment file. DNS targets, TLS paths, htpasswd path, provider credentials,
and server environment remain externally injected and are not committed.

## Prerequisites

- Bash, `jq`, `curl`, Docker Engine, and Docker Compose with `up --wait` support.
- `flock`, plus either `sha256sum` or `shasum`, on the deployment host.
- A systemd/logind host with the Docker rootless extras, `rootlesskit`,
  `slirp4netns`, setuid-root `newuidmap`/`newgidmap`, enabled unprivileged user
  namespaces, and one subordinate UID and GID range of at least 65536 IDs for
  `speakup-staging-runtime`. Missing rootless prerequisites fail closed; the
  contract never connects to `/var/run/docker.sock` as a fallback.
- Nginx with the HTTP SSL, proxy, and Basic Auth modules for edge rendering and
  installation.
- A certificate whose SANs are exactly both Staging hostnames, issued and
  verified through [`deploy/tls/`](../tls/README.md).
- Populated runtime and edge environment files, a populated htpasswd file, and
  a real Staging Server environment file.
- Registry credentials with read access to the two GHCR images.
- A real, current-UID-owned `STAGING_PUBLIC_ROOT` that is not group- or
  world-writable.

The Server environment file must contain the real Staging provider and secret
configuration required by the application. Use the repository `.env.example`
only as a key reference; do not copy local defaults as deployment values and do
not disable OSS or OCR to bypass missing configuration. It must not define
`DATABASE_URL`, `SERVER_HOST`, or `SERVER_PORT`; Compose owns those three values.

## Restricted CI host bootstrap

The root operator installs the host boundary from a reviewed checkout. Build
the broker without embedding the checkout path, record the exact commit, and
pass the CI public key (never its private key) explicitly:

```sh
cd server
CGO_ENABLED=0 go build -trimpath \
  -o /tmp/speakup-staging-broker ./cmd/staging-broker
cd ..
contract_revision="$(git rev-parse HEAD)"
sudo ./deploy/staging/host/bootstrap.sh \
  --broker-binary /tmp/speakup-staging-broker \
  --contract-directory "$(pwd -P)/deploy/staging" \
  --contract-revision "$contract_revision" \
  --ssh-public-key-file /secure/operator-input/staging-ci.pub
```

`bootstrap.sh` accepts each of those four flags exactly once and rejects all
other arguments. It creates or validates only `speakup-staging-ci` and
`speakup-staging-runtime`, installs immutable `manage.sh` and `compose.yaml`
snapshots below
`/opt/xe3-speakup-staging-control/releases/<commit>/deploy/staging/`, and
atomically points the root-owned `current` symlink at that commit. The fixed
broker is `/usr/local/libexec/speakup-staging-broker`; its state is
`/var/lib/speakup/staging-broker`, and the shared lock directory is
`/run/lock/xe3-speakup-staging`.

The CI account has a locked password, a root-owned non-writable home, no
supplementary groups, and `/bin/bash` only because OpenSSH evaluates
`ForceCommand` through the account shell. The `Match User` policy and the
authorized key's `restrict` option both disable passwords, keyboard-interactive
authentication, PTY, agent, TCP/stream-local/X11 forwarding, tunnels, and user
RC files. The forced gate rejects every non-empty `SSH_ORIGINAL_COMMAND`, so
shell, `scp`, `sftp`, and arbitrary remote commands cannot reach the broker.
The sole sudo rule is:

```text
speakup-staging-ci ALL=(speakup-staging-runtime) NOPASSWD: /usr/local/libexec/speakup-staging-broker ""
```

In sudoers, the trailing `""` means exactly zero command arguments; it does not
pass an empty argument. The runtime account remains `nologin`, has no Docker
group, and uses only `unix:///run/user/<runtime-uid>/docker.sock`. Its user
service pins `HOME=/var/lib/speakup/staging-runtime`, the rootless socket, and
the Docker data root. Neither identity may read or write a rootful Docker
socket.

The bootstrap does not create `staging-runtime.env`, the referenced Server
environment, a GHCR credential, an SSH private key, or any provider Secret.
After it succeeds, the root operator must install the first two files as
`speakup-staging-runtime:speakup-staging-runtime` with mode `0600`, then create
the runtime-owned registry credential without putting the token on the command
line:

```sh
sudo install -o speakup-staging-runtime -g speakup-staging-runtime -m 0600 \
  /secure/operator-input/staging-runtime.env /etc/speakup/staging-runtime.env
sudo install -o speakup-staging-runtime -g speakup-staging-runtime -m 0600 \
  /secure/operator-input/staging-server.env /etc/speakup/staging-server.env
sudo runuser -u speakup-staging-runtime -- env -i \
  HOME=/var/lib/speakup/staging-runtime \
  XDG_RUNTIME_DIR="/run/user/$(id -u speakup-staging-runtime)" \
  DOCKER_HOST="unix:///run/user/$(id -u speakup-staging-runtime)/docker.sock" \
  PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  docker login ghcr.io
sudo ./deploy/staging/host/validate.sh
```

The validator checks effective sshd and sudo policy, locked identities, exact
owners and modes, the immutable control snapshot, private runtime inputs,
broker state, the rootless user service/socket/data root, and effective denial
of CI access. It fails if the runtime credential or any rootless prerequisite
is missing; it never tries a rootful daemon.

The GitHub Environment is exactly `staging`, allows only branch `main`, and has
no required reviewer. Its current approved connection is represented outside
the repository by these Environment values:

| Kind | Name | Current value or responsibility |
| --- | --- | --- |
| Variable | `STAGING_DEPLOY_HOST` | `149.71.241.71` (current host; not hard-coded in deployment code) |
| Variable | `STAGING_DEPLOY_PORT` | `22` |
| Variable | `STAGING_DEPLOY_USER` | `speakup-staging-ci` |
| Secret | `STAGING_DEPLOY_SSH_PRIVATE_KEY` | Private half of the single restricted authorized key |
| Secret | `STAGING_DEPLOY_KNOWN_HOSTS` | Out-of-band-verified host-key line used as the SSH integrity anchor |

To replace the Staging server, bootstrap and fully validate the new host with a
new SSH key, verify its host key out of band, update all five Environment
values, run one successful Staging deployment and verification, and only then
revoke the old authorized key and retire the old host. Do not reuse a host key
or copy the old rootless Docker socket, broker state, runtime environment, or
registry credential as an unreviewed shortcut.

## 1. Acquire a manifest and split configuration

Download the manifest artifact from the selected, successful Release Candidate
run. For example:

```sh
gh run download RUN_ID \
  --repo 1024XEngineer/XE3-ESL \
  --name speakup-v0.1.1-release-manifest \
  --dir /opt/speakup/releases/v0.1.1
```

Copy the two examples to locations outside the repository, populate every blank
value, and restrict both files plus the Server environment:

```sh
install -d -o root -g speakup-staging-runtime -m 0710 /etc/speakup
install -o speakup-staging-runtime -g speakup-staging-runtime -m 0600 \
  deploy/staging/staging-runtime.env.example \
  /etc/speakup/staging-runtime.env
install -m 0600 deploy/staging/staging-edge.env.example \
  /etc/speakup/staging-edge.env
install -o speakup-staging-runtime -g speakup-staging-runtime -m 0600 \
  /secure/source/staging-server.env \
  /etc/speakup/staging-server.env
install -d -m 0755 /var/www/speakup-staging-public
install -d -o speakup-staging-runtime -g speakup-staging-runtime -m 0700 \
  /run/lock/xe3-speakup-staging
```

The root operator installs runtime inputs for `speakup-staging-runtime`; trusted
edge operations may remain root-owned and separate.
`staging-runtime.env`, `staging-edge.env`, the Server environment, and htpasswd
file must be regular, non-symlink files owned by that UID with mode `0400` or
`0600`. The TLS private key may be a regular file or Certbot's stable
`live/.../privkey.pem` symlink; its resolved target must be regular, non-empty,
owned by that UID, and mode `0400` or `0600`. The public certificate must be
non-empty, owned by that UID, and not group- or world-writable. The ACME webroot
must be a non-symlink directory owned by that UID with mode `0700`, `0750`, or
`0755`. Runtime validation finishes before a Docker pull, migration, container
update, or shutdown; edge validation finishes before a rendered output is
written. Keep the stable `live/` paths so Certbot renewal can advance their
archive targets; never pin an `archive/.../privkeyN.pem` generation.

Every ancestor of these paths must be a real directory owned by `root` or the
execution UID and must not be group- or world-writable. A sticky directory
owned by one of those UIDs (for example `/tmp`) is the only writable-ancestor
exception. Only the final TLS certificate and private-key components may be
Certbot `live/*.pem` symlinks; their resolved `archive/` targets and target
ancestors are checked under the same boundary. Any other symbolic-link path
component fails closed.

The two configuration contracts are exact and non-overlapping:

| File | Exact keys and responsibility | Commands | Required access |
| --- | --- | --- | --- |
| `staging-runtime.env` | `STAGING_POSTGRES_DB`, `STAGING_POSTGRES_USER`, `STAGING_POSTGRES_PASSWORD`, `PORTAL_ADMIN_PASSWORD`, `STAGING_SERVER_ENV_FILE`; Compose, migration, loopback health, runtime verification, and receipt inputs | `validate`, `deploy`, `verify`, `status`, `down` with `--runtime-env-file` | Read the manifest, runtime env, and referenced Server env; use the Staging Docker/Compose runtime; use the private deployment lock and write a new receipt where applicable. No edge file or edge Secret access is required. |
| `staging-edge.env` | `STAGING_TLS_CERTIFICATE`, `STAGING_TLS_CERTIFICATE_KEY`, `STAGING_HTPASSWD_FILE`, `STAGING_ACME_ROOT`, `STAGING_PUBLIC_ROOT`; strict Nginx template rendering only | `render-nginx` with `--edge-env-file` and `--output` | Read the edge env and referenced TLS/htpasswd inputs, traverse the ACME/public roots, and create the requested output. No manifest, runtime/Server env, receipt, Docker, or Compose access is required. |

Unknown, duplicate, or cross-contract keys fail closed. Values are read only
from the selected file; exported ambient variables do not fill missing keys.
Error output identifies the key or line but never prints a Secret value. The
fixed Portal and API hostnames are injected by the repository contract and must
not be added to either file.

The dedicated lock directory is mandatory and must be recreated at boot when
`/run` is volatile. The deployment script never creates this directory and
fails closed if it is missing, a symlink, owned by another UID, or group/world
writable. Only the lock file inside that already-private directory may be
created by the script.

Before public verification, create A records for both selected hostnames (and
AAAA records only when the host is intentionally IPv6-reachable) and point them
to the Staging host. IP addresses stay in the DNS/operations system, not this
repository.

The TLS lifecycle contract first renders a port-80-only bootstrap include and
then issues the exact Staging lineage. Populate this deployment environment with
the same live certificate paths derived from `TLS_CERTBOT_CONFIG_ROOT`; do not
create a second Certbot account or certificate root for this stack.

`STAGING_POSTGRES_PASSWORD` accepts URL-safe characters and must contain at
least 24 characters. A suitable value can be generated with
`openssl rand -hex 32`. Create the Portal access file with the host's `htpasswd`
tool; neither plaintext credentials nor the generated file belong in the
repository. Do not add HTTP Basic Auth to the API host: the Staging APK uses its
`Authorization: Bearer` header for application identity.

### Migrate from the legacy combined environment

The old `staging.env` and `--env-file` interface have no compatibility fallback.
Migrate explicitly:

1. create `/etc/speakup/staging-runtime.env` and
   `/etc/speakup/staging-edge.env` from the committed examples;
2. copy only the five allowlisted runtime keys and five allowlisted edge keys
   from the old file; remove `STAGING_PORTAL_HOST` and `STAGING_API_HOST` because
   both hostnames are now fixed repository contracts;
3. keep both new files owned by their executing UID with mode `0400` or `0600`,
   and preserve the existing ownership, mode, and ancestor checks for every
   referenced path;
4. replace runtime invocations with `--runtime-env-file` and the Nginx renderer
   with `--edge-env-file`;
5. run runtime `validate` and edge `render-nginx` independently before removing
   the old combined file.

Passing `--env-file` fails with the appropriate migration flag. Do not export
the old values into the process environment as a workaround.

## 2. Validate without changing containers

```sh
./deploy/staging/manage.sh validate \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --runtime-env-file /etc/speakup/staging-runtime.env
```

Validation accepts only the canonical v1 Release Candidate manifest with its
exact key set; missing, unknown, or multiple JSON documents fail closed. It
also fails on a malformed manifest, mutable or placeholder image reference,
missing runtime value, invalid runtime or Server input, or invalid Compose
model. It does not read the edge env, validate edge files, render Nginx, pull
images, or contact the application providers.

## 3. Deploy the immutable Staging images

```sh
./deploy/staging/manage.sh deploy \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --runtime-env-file /etc/speakup/staging-runtime.env \
  --receipt /opt/speakup/releases/v0.1.1/staging-deployment-receipt.json
```

The command performs the following fail-closed sequence:

1. validate the manifest, runtime configuration, Server env, and Compose model,
   and require a new receipt path without reading edge inputs;
2. acquire the exclusive
   `/run/lock/xe3-speakup-staging/deploy.lock` lock shared by `deploy` and
   `down` (the test harness alone overrides this path);
3. pull the pinned PostgreSQL, Portal, and Server images;
4. start only PostgreSQL with `--pull never` and wait for its health check;
5. run `/usr/local/bin/speakup-migrate up` with `--pull never`, then run
   `speakup-migrate version` and require the sole output to be exactly
   `version=N dirty=false`, with `N` equal to `database_schema_version` in the
   manifest;
6. only after that exact schema check, update Portal and Server with
   `--pull never` and wait for their container health checks;
7. verify the exact project service set, running container identity and health,
   pinned `Config.Image`, inspectable Linux/amd64 image and release OCI labels,
   loopback ports, networks, internal database isolation, Staging volumes, the
   live schema, Portal `/`, Server `/health`, and Server `/readyz`;
8. atomically create the new receipt without overwriting any existing path.

The receipt binds the manifest SHA-256, release version, Git SHA, schema,
Portal and Server image digests, exact container IDs, and UTC deployment time.
It contains no credentials. Its parent directory must already exist, be owned
by the executing UID, and have mode `0700`, `0750`, or `0755`.

If migration fails, the command exits before the Portal or Server update. A
dirty or mismatched schema, invalid runtime, or failed application health check
also prevents the receipt. Schema migrations are not automatically reversed:
rolling an application image back across an unknown schema boundary is unsafe,
so recovery requires the reviewed database restore or migration procedure for
that release. This contract does not add Production promotion.

## 4. Render and install the Nginx configuration

Rendering writes only the requested output file. It never reloads Nginx:

```sh
./deploy/staging/manage.sh render-nginx \
  --edge-env-file /etc/speakup/staging-edge.env \
  --output /opt/speakup/releases/v0.1.1/staging-nginx.conf
```

`render-nginx` validates only the exact edge allowlist and its referenced
certificate, private key, htpasswd, ACME root, and public root. It uses the two
fixed repository hostnames, requires no manifest or runtime/Server env, and
does not invoke Docker Compose. Runtime flags, `--manifest`, and `--receipt`
fail before an output is written.

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
  --runtime-env-file /etc/speakup/staging-runtime.env

./deploy/staging/manage.sh verify \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --runtime-env-file /etc/speakup/staging-runtime.env

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

`verify` never pulls an image. It first verifies the exact runtime project,
services, image digests and OCI labels, networks, ports, and volumes; it then
runs the migration image with `--pull never` to confirm the live schema before
checking the three loopback endpoints.

## Release business UAT

Preview the immutable five-session matrix without making any network request:

```sh
node deploy/staging/uat.mjs
```

After the candidate is deployed, create a private receipt directory and opt in
to the real Staging-only run explicitly:

```sh
receipt_directory="$(mktemp -d)"
receipt_directory="$(cd "$receipt_directory" && pwd -P)"
UAT_ENV=staging node deploy/staging/uat.mjs \
  --execute \
  --base-url https://staging-api.speak-up.top \
  --receipt "$receipt_directory/receipt.json"
```

The executor creates a one-time account, dynamically freezes the current IELTS
question assignments, and exercises PART 1 text/voice, PART 2 voice, PART 3
voice, and FULL MOCK text through the terminal session report. Voice cases use
the repository's fixed WAV fixture only as transport/ASR evidence. The `0600`
receipt contains hashed resource references and status/timing metadata; it
does not persist credentials, account identifiers, questions, answers,
transcripts, audio, or provider payloads. Redirects, non-Staging origins,
disabled TLS verification, failed evaluations, invalid evidence, and bounded
polling timeouts fail closed.
The receipt directory must already exist, be owned by the invoking user, have
mode `0700`, and resolve without symbolic-link ancestors.

## 6. Stop or clean up Staging

```sh
./deploy/staging/manage.sh down \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --runtime-env-file /etc/speakup/staging-runtime.env
```

`down` removes only the `xe3-speakup-staging` containers and networks and keeps
`xe3-speakup-staging_portal_data` and
`xe3-speakup-staging_postgres_data`. Remove the rendered Nginx include and
reload Nginx if the public Staging entry points should also disappear. Volume
deletion is intentionally not automated; inspect and back up these explicitly
Staging-scoped volumes before deleting them by name.

`down` uses the same exclusive lock as `deploy` and fails rather than running
unlocked or concurrently with a release.

## Security boundary and non-goals

CI reaches only the Staging-scoped forced gate and no-argument broker. The
broker runs as `speakup-staging-runtime`, not root, and receives only the fixed
rootless Docker socket, runtime environment, state, lock, and immutable control
paths. CI cannot read those files or traverse the runtime state. Edge rendering
and Nginx installation remain a trusted host-operator responsibility; the
broker does not receive the edge env, TLS private key, htpasswd, arbitrary
filesystem paths, a shell, or a generic Docker command.

This contract does not configure Production, DNS, OSS, Nginx, certificates,
firewalls, a China-region backend, or application routing. It does not create
GitHub Secrets, host runtime/provider Secrets, or a rootful-Docker fallback.
Those changes require their own reviewed Issues and must not broaden this
Staging identity's capability.

## Reproducible contract checks

```sh
make check-android-download
make check-staging-deploy
make check-staging-host-access
make check-staging-nginx
make check-tls-lifecycle
```

`check-staging-deploy` covers the runtime allowlist, private input
ownership/modes and symlink rejection, lock conflicts, manifest failures,
Compose model, schema parsing, runtime identity, no-pull verification, endpoint
checks, and atomic no-clobber receipts without edge inputs.
`check-staging-host-access` exercises the root-operator bootstrap in an isolated
fixture, validates the exact sshd/sudoers/user/rootless/file-mode contract, and
proves that shell/scp/sftp, broker arguments, unrestricted keys, supplementary
groups, public runtime inputs, and an unsafe or missing rootless socket fail
closed without touching a real host.
`check-staging-nginx` covers the edge allowlist and private-path validation,
renders without runtime inputs or Compose, and runs `nginx -t` in a pinned
Docker Official Nginx image. The other two commands retain the adjacent Android
publication and TLS lifecycle contracts; their edge paths and behavior are
unchanged.

## Official references

- [Docker Compose image and healthcheck reference](https://docs.docker.com/reference/compose-file/services/)
- [Docker Compose startup order](https://docs.docker.com/compose/how-tos/startup-order/)
- [Docker Compose project isolation](https://docs.docker.com/compose/how-tos/project-name/)
- [Docker Engine security and daemon attack surface](https://docs.docker.com/engine/security/)
- [Docker group root-level privileges](https://docs.docker.com/engine/install/linux-postinstall/)
- [Docker Official PostgreSQL image storage contract](https://github.com/docker-library/docs/blob/master/postgres/content.md)
- [Nginx proxy module](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)
- [Nginx TLS module](https://nginx.org/en/docs/http/ngx_http_ssl_module.html)
- [Nginx Basic Auth module](https://nginx.org/en/docs/http/ngx_http_auth_basic_module.html)
- [GitHub Actions artifact download](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/download-workflow-artifacts)
- [GitHub secure use of self-hosted runners](https://docs.github.com/en/actions/reference/security/secure-use)
