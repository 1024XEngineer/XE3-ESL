# SpeakUp TLS lifecycle contract

This directory defines the audited HTTP-01 issuance, verification, activation,
and renewal path for the three SpeakUp certificate lineages. Committing or merging
these files does **not** connect to a server, contact a certificate authority,
change DNS, install Nginx configuration, or switch any traffic.

## Fixed contract

- The only ACME client image is the Docker Official Image
  `certbot/certbot:v5.7.0`, pinned by its multi-platform index digest as
  `certbot/certbot@sha256:34ee91d2f43008eb78a007d22f23ed4b2eaa9a454cb27ca2c042b49527a695b4`.
- Certbot runs only on a `linux/amd64` host and the container platform is
  explicitly `linux/amd64`.
- Registry access is confined to the explicit `prepare-image` installation or
  upgrade command. It pulls that platform and digest, then inspects the local
  image for `linux/amd64` and the exact repository digest. Issuance, expansion,
  renewal, dry-run, and the systemd service all use `--pull never`; a missing or
  mismatched local image fails closed.
- Staging is a new lineage named `staging.speak-up.top` with exactly
  `staging.speak-up.top` and `staging-api.speak-up.top`.
- Production expands the existing `speak-up.top` lineage. Before expansion its
  SANs must be exactly `speak-up.top` plus `www.speak-up.top`; an already
  expanded exact three-name certificate is accepted idempotently. A missing
  lineage, a partial lineage, or any other SAN is rejected. The result must
  contain exactly `speak-up.top`, `www.speak-up.top`, and `api.speak-up.top`.
- Monitor is a separate lineage named `monitor.speak-up.top` with exactly that
  one SAN. It reuses the reviewed Production ACME account and webroot but never
  expands or otherwise changes the Production three-name lineage.
- New issuance never registers or guesses an ACME account. Staging and monitor
  issuance plus Production expansion reuse the exact account referenced by the existing
  Production renewal configuration, validate its fixed Let's Encrypt storage
  path and file permissions, pass it explicitly with `--account`, and require
  all three saved renewal configurations to retain that same account.
- Both Production's existing Portal hosts and its new API host use the same
  operator-provided Production webroot during expansion and renewal.
- A certificate is usable only when the Certbot live paths resolve inside the
  expected archive lineage, the real private key grants no group/other access,
  the leaf and private key public keys match, the SAN set is exact, and at least
  seven days of validity remain.
- Before renewal, the same SAN, key, path, and permission checks apply, but a
  certificate with less than seven days remaining may still reach Certbot while
  at least five minutes remain. This fail-closed clock-skew window never treats
  an expired or imminently expiring certificate as valid. Any certificate checked
  after a live renewal must again have at least seven days remaining.
- Before activation, every final Staging, Production, or monitor hostname must occur in
  exactly one `443 ssl` server block. That block itself must contain exactly one
  direct certificate and key directive for the current lineage; directives in
  nested or unrelated blocks cannot satisfy the check. A still-installed HTTP
  bootstrap or legacy Portal-only vhost therefore cannot be recorded as the
  final activated state.
- `renew` validates all three lineages before and after Certbot runs. Nginx reloads
  once only when a verified certificate differs from the last successfully
  activated SHA-256 and `nginx -t` succeeds. A dry-run never reloads.
- Renewal configuration must still use the webroot authenticator and Let's
  Encrypt production endpoint. Stored pre/post/renew/deploy hooks are rejected
  and Certbot directory hooks are disabled. The container uses an empty explicit
  global configuration file, so no unreviewed global option or hook can bypass
  the wrapper's verification-before-reload sequence.
- `autorenew` must be absent (Certbot's documented enabled default) or exactly
  `True`. The real `[[webroot_map]]` must contain every certificate SAN exactly
  once, no other key, and map each one to `/var/www/acme`.

ACME account data, certificates, private keys, the populated environment file,
and deployed SHA state are host data and must not be committed.

## 1. Install the configuration contract

Run these commands as `root` on the target Ubuntu amd64 host. Preserve the
existing Production Certbot configuration root so that the current account,
lineage, and renewal history are expanded instead of replaced.

```sh
install -d -o root -g root -m 0755 /etc/speakup
install -d -o root -g root -m 0700 /var/lib/speakup/tls
install -d -o root -g root -m 0755 /var/www/staging-acme
chmod 0700 /opt/xe3-speakup-portal/certbot/conf
install -o root -g root -m 0600 \
  deploy/tls/tls.env.example /etc/speakup/tls.env
install -o root -g root -m 0755 \
  deploy/tls/manage.sh /usr/local/sbin/xe3-speakup-tls
install -o root -g root -m 0644 \
  deploy/tls/nginx-http.conf.template \
  /usr/local/sbin/nginx-http.conf.template
```

Confirm that `TLS_CERTBOT_CONFIG_ROOT` points to the existing Production Certbot
root and that `TLS_PRODUCTION_ACME_ROOT` is the webroot already served by the
current `speak-up.top` and `www.speak-up.top` port-80 vhosts. That root must
contain the Production renewal file and its referenced Let's Encrypt production
account; the wrapper refuses to register a replacement account or select one by
directory order. The example values match the recorded legacy Portal layout.
The configuration parser accepts only the five documented raw `KEY=value`
entries, rejects duplicates and shell syntax, and never prints values from a
rejected line.

Prepare the reviewed image in a separate, auditable installation step:

```sh
/usr/local/sbin/xe3-speakup-tls prepare-image \
  --env-file /etc/speakup/tls.env
```

This is the only lifecycle command allowed to contact the container registry.
It runs `docker pull --platform linux/amd64` for the fixed digest and rejects the
result unless `docker image inspect` reports `linux/amd64` and includes that
exact digest in `RepoDigests`. Repeat it only after an approved digest update;
scheduled renewal never upgrades software implicitly.

The final Staging, Production, and monitor configurations must reference the
live paths derived from this root:

```text
<TLS_CERTBOT_CONFIG_ROOT>/live/staging.speak-up.top/{fullchain,privkey}.pem
<TLS_CERTBOT_CONFIG_ROOT>/live/speak-up.top/{fullchain,privkey}.pem
<TLS_CERTBOT_CONFIG_ROOT>/live/monitor.speak-up.top/{fullchain,privkey}.pem
```

## 2. Install HTTP-01 bootstrap vhosts

Let's Encrypt HTTP-01 fetches
`http://<domain>/.well-known/acme-challenge/<token>` on public port 80. The
bootstrap template serves only that path from the selected webroot; every other
path returns `404`, and it does not listen on 443. It also carries an inert
environment marker that makes activation fail until the temporary vhost is
removed from the loaded Nginx configuration.

Render Staging's two new hostnames:

```sh
/usr/local/sbin/xe3-speakup-tls render-bootstrap \
  --environment staging \
  --env-file /etc/speakup/tls.env \
  --output /usr/local/nginx/conf/vhost/xe3-speakup-staging-bootstrap.conf
```

Production's bootstrap include contains only `api.speak-up.top`. It deliberately
does not duplicate the existing `speak-up.top` or `www.speak-up.top` server
blocks; those existing blocks must already serve the same Production webroot.

```sh
/usr/local/sbin/xe3-speakup-tls render-bootstrap \
  --environment production \
  --env-file /etc/speakup/tls.env \
  --output /usr/local/nginx/conf/vhost/xe3-speakup-api-bootstrap.conf
/usr/local/nginx/sbin/nginx -t
/usr/local/nginx/sbin/nginx -s reload
```

Monitor has one independent hostname but uses the reviewed Production webroot:

```sh
/usr/local/sbin/xe3-speakup-tls render-bootstrap \
  --environment monitor \
  --env-file /etc/speakup/tls.env \
  --output /usr/local/nginx/conf/vhost/xe3-speakup-monitor-bootstrap.conf
/usr/local/nginx/sbin/nginx -t
/usr/local/nginx/sbin/nginx -s reload
```

Before contacting the CA, place a temporary non-secret file below each
webroot's `.well-known/acme-challenge/` directory and verify that every selected
hostname returns that exact file on HTTP while an unrelated path returns `404`.
Remove the file after verification. Do not issue while an AAAA record points to
an unreachable or differently configured host.

## 3. Issue Staging and monitor, then expand Production

After DNS and public port 80 have been independently verified:

```sh
/usr/local/sbin/xe3-speakup-tls issue-staging \
  --env-file /etc/speakup/tls.env

/usr/local/sbin/xe3-speakup-tls issue-monitor \
  --env-file /etc/speakup/tls.env

/usr/local/sbin/xe3-speakup-tls expand-production \
  --env-file /etc/speakup/tls.env
```

Neither command installs an HTTPS vhost or reloads Nginx. Each command uses
only the fixed lineage, SANs, Let's Encrypt production endpoint, and explicitly
validated existing Production account. It then verifies the resulting live
certificate and persisted renewal account and prints a small audit record
containing its environment, lineage, SHA-256, expiry, and `reload=false`. It does
not print the environment file, account identifier, account key, or private key.

If either issue command exits after Certbot has already reported a saved
certificate, do not rerun it blindly: the exact new lineage may now exist and
rate limits still apply. Record hashes and permissions for its exact `live`,
`archive`, and `renewal` paths, then run `verify` for that environment.
Repair a local permission or renewal-account mismatch in place when the
certificate itself is valid. If the lineage is incomplete or irreparably wrong,
move only those three exact Staging paths to a root-owned timestamped quarantine
outside the Certbot root before one controlled retry. Never move, delete, or
revoke the `speak-up.top` Production lineage as part of Staging recovery.

The recorded legacy Production renewal map may contain exactly
`speak-up.top` and `www.speak-up.top` at `/var/www/certbot`. Only
`expand-production` accepts that one legacy shape. Before requesting the
expanded certificate, it invokes Certbot's official `reconfigure` flow with the
canonical `/var/www/acme` path. Certbot reconstructs the identifiers from the
current certificate and treats an explicitly supplied webroot path as a map
override; the wrapper then verifies that the live certificate did not change
and requires the resulting exact two-name map. The expansion request passes an
explicit exact three-name webroot map, and its saved renewal configuration must
pass the strict contract afterward. Missing, extra, duplicate, mixed, or other
legacy mappings fail before Certbot. `verify`, `activate`, `renew`, and
`renew-dry-run` never accept the legacy path.

Render the final HTTPS configurations with `deploy/staging/manage.sh` and
`deploy/production/manage.sh`, review them, and replace the temporary bootstrap
includes. Never leave a bootstrap include installed beside a final vhost with
the same `server_name`. Test and atomically activate each verified certificate:

```sh
/usr/local/sbin/xe3-speakup-tls activate \
  --environment staging \
  --env-file /etc/speakup/tls.env

/usr/local/sbin/xe3-speakup-tls activate \
  --environment production \
  --env-file /etc/speakup/tls.env

/usr/local/sbin/xe3-speakup-tls activate \
  --environment monitor \
  --env-file /etc/speakup/tls.env
```

`activate` runs all certificate and renewal checks, `nginx -t`, and a private
inspection of the loaded Nginx configuration before a graceful reload, then
records the activated certificate SHA-256 in `/var/lib/speakup/tls`. If testing,
the loaded-vhost contract, or reload fails, no state is advanced.

Installing the final Production template and running Production `activate`
changes the live reverse proxy and therefore belongs to the separately approved
Production cutover. This Issue and its PR stop at the tested host-side contract.

## 4. Dry-run and automatic renewal

Only after all final HTTPS vhosts have been activated, test all three lineages
against Certbot's renewal staging flow:

```sh
/usr/local/sbin/xe3-speakup-tls renew-dry-run \
  --env-file /etc/speakup/tls.env
```

Dry-run success does not modify a live certificate, deployed SHA state, or
Nginx. Before enabling the new timer, inspect `systemctl list-timers`, root/user
crontabs, and `/etc/cron.d`. Move any installed legacy
`xe3-speakup-certbot` cron file to a secured backup and verify that no scheduler
still invokes `xe3-renew-cert`; the legacy job used a mutable image and reloaded
unconditionally, so it must not run beside this contract.

Install and verify the fixed systemd units, then enable the timer:

```sh
install -o root -g root -m 0644 \
  deploy/tls/xe3-speakup-tls-renew.service \
  deploy/tls/xe3-speakup-tls-renew.timer \
  /etc/systemd/system/
systemctl daemon-reload
systemd-analyze verify \
  /etc/systemd/system/xe3-speakup-tls-renew.service \
  /etc/systemd/system/xe3-speakup-tls-renew.timer
systemctl start xe3-speakup-tls-renew.service
systemctl enable --now xe3-speakup-tls-renew.timer
```

The timer runs twice daily with up to 30 minutes of randomized delay and catches
up after downtime. The script uses a non-blocking `flock`, while Certbot's own
random sleep is disabled because scheduling jitter belongs to systemd. The unit
cannot pull an image; if the prepared digest is absent or wrong it fails and
waits for an operator to run the separately audited `prepare-image` command.
Let's Encrypt stopped its certificate-expiration email service in June 2025 and
no longer stores new ACME account contact addresses. Email therefore cannot be
the renewal safety net: production monitoring must independently alert on timer
failure and certificate validity before the seven-day release threshold.

## 5. Verification, evidence, and recovery

After activation, verify all six public HTTPS hostnames from outside the server,
including their expected redirects, Portal/API health behavior, certificate SANs,
and expiry. Record only non-secret evidence:

```sh
curl --fail --user staging-user https://staging.speak-up.top/
curl --fail https://staging-api.speak-up.top/health
curl --fail --location https://speak-up.top/
curl --silent --show-error --head https://www.speak-up.top/
curl --fail https://api.speak-up.top/health
curl --fail https://monitor.speak-up.top/api/health

openssl s_client \
  -connect staging.speak-up.top:443 \
  -servername staging.speak-up.top \
  -verify_return_error </dev/null 2>/dev/null |
  openssl x509 -noout -ext subjectAltName -dates -fingerprint -sha256
openssl s_client \
  -connect speak-up.top:443 \
  -servername speak-up.top \
  -verify_return_error </dev/null 2>/dev/null |
  openssl x509 -noout -ext subjectAltName -dates -fingerprint -sha256
openssl s_client \
  -connect monitor.speak-up.top:443 \
  -servername monitor.speak-up.top \
  -verify_return_error </dev/null 2>/dev/null |
  openssl x509 -noout -ext subjectAltName -dates -fingerprint -sha256

/usr/local/sbin/xe3-speakup-tls verify \
  --environment staging --env-file /etc/speakup/tls.env
/usr/local/sbin/xe3-speakup-tls verify \
  --environment production --env-file /etc/speakup/tls.env
/usr/local/sbin/xe3-speakup-tls verify \
  --environment monitor --env-file /etc/speakup/tls.env
systemctl list-timers xe3-speakup-tls-renew.timer
systemctl status xe3-speakup-tls-renew.service
journalctl -u xe3-speakup-tls-renew.service --since today
```

If Certbot fails, any lineage fails verification, or `nginx -t` fails,
`renew` exits nonzero without a reload and the existing Nginx workers continue
using their previously loaded certificate. Fix the CA, DNS, file permission,
certificate, or Nginx error and run `renew` again. If Certbot had already updated
a live symlink before a later check failed, the unchanged deployed SHA record
causes the next fully successful run to test, reload, and record that pending
certificate rather than silently skipping it.

Local and CI verification never contacts a real CA or writes a server. It runs
the lifecycle boundaries against fake Docker/OpenSSL/Nginx tools, then syntax
checks all three rendered bootstrap fragments in the same pinned Docker Official
Nginx image used by the deployment contracts:

```sh
make check-tls-lifecycle
make check-staging-deploy
make check-staging-nginx
make check-production-deploy
make check-production-nginx
```

Implementation references:

- Certbot 5.7 [webroot and renewal guide](https://eff-certbot.readthedocs.io/en/stable/using.html)
  and [CLI reference](https://eff-certbot.readthedocs.io/en/stable/man/certbot.html)
- Certbot v5.7.0 source for
  [`reconfigure`](https://github.com/certbot/certbot/blob/v5.7.0/certbot/src/certbot/_internal/main.py),
  [renewal restoration](https://github.com/certbot/certbot/blob/v5.7.0/certbot/src/certbot/_internal/renewal.py),
  and the [webroot map](https://github.com/certbot/certbot/blob/v5.7.0/certbot/src/certbot/_internal/plugins/webroot.py)
- Docker Hub's [official Certbot v5.7.0 image](https://hub.docker.com/r/certbot/certbot/tags)
- Docker [pull by digest and `--platform`](https://docs.docker.com/reference/cli/docker/image/pull/)
  and [`docker image inspect`](https://docs.docker.com/reference/cli/docker/image/inspect/)
- Let's Encrypt [HTTP-01 challenge](https://letsencrypt.org/docs/challenge-types/)
- Let's Encrypt [ending expiration notification emails](https://letsencrypt.org/2025/01/22/ending-expiration-emails.html)
  and the current [expiration email status](https://letsencrypt.org/docs/expiration-emails/)
- Nginx [configuration test and graceful reload](https://nginx.org/en/docs/control.html)
- systemd [`systemd.service`](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)
  and [`systemd.timer`](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html)
