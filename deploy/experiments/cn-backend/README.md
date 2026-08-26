# China backend latency experiment

This Compose stack runs only the SpeakUp Server and an isolated PostgreSQL
database. It exists to compare a China-hosted backend with the current
Singapore Staging backend before any Production traffic or data is moved.

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
