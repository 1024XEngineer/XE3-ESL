# China backend latency experiment

This Compose stack runs only the SpeakUp Server and an isolated PostgreSQL
database. It exists to compare a China-hosted backend with the current
Singapore Staging backend before any Production traffic or data is moved.

Formal migration evidence and approval are tracked with
[`cutover-checklist-v1.md`](./cutover-checklist-v1.md). An incomplete checklist
means this stack must remain isolated from Production traffic.

The stack deliberately uses its own Compose project, volume, port, deployment
directory, runtime file, and Server environment file. It must not reuse a
Staging or Production database volume. The Server image digest must come from
the same successful Release Candidate manifest used by the comparison APK.

This stack binds the container API only to host loopback port `28083`; it never
publishes PostgreSQL or metrics. Do not point the APK at a public plaintext
mapping. Until a trusted TLS edge exists, connect a debug APK through USB and
an encrypted SSH local forward:

```sh
ssh -N -L 18083:127.0.0.1:28083 -p "$CN_EXPERIMENT_SSH_PORT" \
  "$CN_EXPERIMENT_SSH_TARGET"
adb reverse tcp:18083 tcp:18083
```

The debug APK then uses `http://127.0.0.1:18083`; plaintext exists only across
the local USB forwarding boundary and the Internet leg is protected by SSH.

## Host files

- Compose file: `/opt/speakup-cn-experiment/compose.yaml`
- Runtime configuration: `/etc/speakup-cn-experiment/runtime.env`
- Provider configuration: `/etc/speakup-cn-experiment/server.env`

Both environment files must be owned by root with mode `0600`. The Server
environment must not define `DATABASE_URL`, `SERVER_HOST`, or `SERVER_PORT`;
Compose owns those values.

Run `./validate-runtime.sh /etc/speakup-cn-experiment/runtime.env` before every
Compose operation. The validator requires lowercase PostgreSQL identifiers and
a 24-to-128-character URL-safe password because Compose inserts these values
directly into a PostgreSQL URL.

Generate a unique experiment password rather than copying a placeholder:

```sh
openssl rand -hex 24
```

Both images use `pull_policy: never`. Load the exact Server and PostgreSQL
digests recorded by the selected Release Candidate before starting the stack.
An offline PostgreSQL import may retain only the image ID; verify that ID is
`sha256:7d2695c3aa88e792e8b3b233e7e4adb296a20412c6c0ca361e3edaaacfada108`
before applying the local `postgres:18-bookworm` tag. This keeps the experiment
reproducible when the China host cannot reach Docker Hub and prevents an
unreviewed registry mirror from changing the image.

## Start and verify

```sh
./validate-runtime.sh /etc/speakup-cn-experiment/runtime.env

docker compose \
  --env-file /etc/speakup-cn-experiment/runtime.env \
  --file /opt/speakup-cn-experiment/compose.yaml \
  up --detach postgres

docker compose \
  --env-file /etc/speakup-cn-experiment/runtime.env \
  --file /opt/speakup-cn-experiment/compose.yaml \
  --profile migration run --rm migrate

docker compose \
  --env-file /etc/speakup-cn-experiment/runtime.env \
  --file /opt/speakup-cn-experiment/compose.yaml \
  up --detach --wait postgres server

curl --fail http://127.0.0.1:28083/health
curl --fail http://127.0.0.1:28083/readyz
```

## Current-provider smoke

Run the smoke only on the experiment host after readiness succeeds:

```sh
./smoke-current-providers.sh
```

It creates an isolated test account, verifies registration, login, current
user lookup, the IELTS question bank, one real answer-generation request, one
real TTS request, and logout. It never prints credentials or response bodies.
The account remains only in the isolated experiment database so repeated runs
are auditable. ASR, WebSocket voice streaming, and report generation still
require the comparison APK and a real-device session.

## Private experiment metrics

The optional monitoring overlay runs only Prometheus and a textfile-only
node_exporter. It does not install Grafana, Alertmanager, Nginx, blackbox probes,
or a Docker socket mount. Prometheus is available only on host loopback port
`29091`; Server metrics remain unpublished and are reachable only through the
internal `monitor` Docker network. Prometheus alone also joins an otherwise
empty bridge network so Docker can publish its UI to host loopback; neither the
Server nor node_exporter joins that bridge.

Install the fixed host exporter and timer, then start the overlay with the base
Compose file so it shares the same private network:

```sh
install -d -o root -g root -m 0755 /var/lib/speakup-cn-experiment/metrics
install -m 0755 observability/export-host-metrics.sh \
  /usr/local/sbin/xe3-speakup-cn-experiment-export-metrics
install -m 0644 observability/xe3-speakup-cn-experiment-metrics.service \
  observability/xe3-speakup-cn-experiment-metrics.timer \
  /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now xe3-speakup-cn-experiment-metrics.timer
systemctl start xe3-speakup-cn-experiment-metrics.service

docker compose \
  --env-file /etc/speakup-cn-experiment/runtime.env \
  --file /opt/speakup-cn-experiment/compose.yaml \
  --file /opt/speakup-cn-experiment/compose.observability.yaml \
  up --detach --wait server prometheus node-exporter
```

Inspect Prometheus through an encrypted local forward rather than a public
port:

```sh
ssh -N -L 29091:127.0.0.1:29091 -p "$CN_EXPERIMENT_SSH_PORT" \
  "$CN_EXPERIMENT_SSH_TARGET"
curl --fail http://127.0.0.1:29091/-/ready
```

The host exporter records local health/readiness, exact experiment container
state and restart counts, plus root filesystem bytes and inodes. Application
metrics provide real provider calls, failures, duration, and usage. The smoke
test is deliberately not scheduled because it consumes paid providers and
creates accounts. This private stack cannot detect a complete host outage and
has no notification delivery path; those remain formal HTTPS and Alertmanager
cutover prerequisites.

## Stop without deleting experiment data

```sh
./validate-runtime.sh /etc/speakup-cn-experiment/runtime.env

docker compose \
  --env-file /etc/speakup-cn-experiment/runtime.env \
  --file /opt/speakup-cn-experiment/compose.yaml \
  down
```

Do not add `--volumes` unless the experiment database is intentionally being
destroyed after its results have been recorded.

## Regional provider switch

Keep a mode-`0600` copy of the working Singapore baseline before changing the
active `server.env`. A complete China candidate changes these groups together:

- `QIANWEN_BASE_URL`, `QIANWEN_ASR_BASE_URL`, `QIANWEN_TTS_BASE_URL`, and
  `DASHSCOPE_API_KEY`;
- `OSS_REGION`, `OSS_ENDPOINT`, `OSS_BUCKET`, and the selected OSS credential
  provider values;
- `XFYUN_ISE_ENDPOINT`, `APPID`, `APIKey`, and `APISecret`;
- any explicitly regional avatar or OCR endpoint that remains enabled.

Do not mix a China credential with a Singapore endpoint or reuse the Production
database. After replacing the active environment file, recreate only the
Server container and run the same readiness and real-provider smoke checks.
Rollback restores the known-good baseline environment file and recreates the
Server container; PostgreSQL is unchanged.
