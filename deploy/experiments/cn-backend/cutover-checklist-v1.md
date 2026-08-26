# China backend cutover checklist v1

This is a fail-closed template. A blank item means the China backend is not
approved to receive Production traffic. Never put credentials or raw
environment files in this document or in Git.

```text
plan_version: 1
release_version:
git_sha:
server_image_digest:
database_schema_version:
generated_at_utc:
change_owner:
approver:
private_receipt_root:
```

Private evidence belongs under
`/var/lib/speakup-cn-experiment/receipts/<version>/<timestamp>/` with directory
mode `0700` and file mode `0600`. This checklist records only receipt names and
SHA-256 values.

## 1. Release and resource boundary

- [ ] The current Singapore release completed through immutable Tag, GitHub
  Release, public APK, and changelog verification before this cutover started.
- [ ] China-provider preparation did not change or block the current Singapore
  release path; this checklist governs only the later China cutover.
- [ ] Candidate `release_version`, Git SHA, Server digest, PostgreSQL digest,
  and schema version match the reviewed Release Candidate manifest.
- [ ] China Qianwen, OSS, and XFYun resources are Production-owned and use
  China endpoints. Enabled avatar or OCR providers were classified explicitly.
- [ ] The active China `server.env` contains no Singapore endpoint mixed with a
  China credential, and its SHA-256 is recorded without exposing values.
- [ ] The known-good Singapore `server.env` SHA-256 and Server image digest are
  recorded as rollback inputs.

Required provider groups change together:

- Qianwen text generation: `TEXT_GENERATION_PROVIDER`,
  `DASHSCOPE_API_KEY`, `QIANWEN_BASE_URL`, `QIANWEN_MODEL`,
  `QIANWEN_EVALUATION_MODEL`, `QIANWEN_SPEECH_FEEDBACK_MODEL`,
  `QIANWEN_TIMEOUT`, and `QIANWEN_MAX_OUTPUT_TOKENS`;
- Qianwen ASR: `SPEECH_RECOGNITION_PROVIDER`, `QIANWEN_ASR_BASE_URL`,
  `QIANWEN_ASR_MODEL`, `QIANWEN_ASR_TIMEOUT`,
  `QIANWEN_ASR_RECORDED_MODEL`, and `QIANWEN_ASR_RECORDED_TIMEOUT`;
- Qianwen TTS: `SPEECH_SYNTHESIS_PROVIDER`, `QIANWEN_TTS_BASE_URL`,
  `QIANWEN_TTS_MODEL`, `QIANWEN_TTS_VOICE`, `QIANWEN_TTS_LANGUAGE`,
  `QIANWEN_TTS_TIMEOUT`, and `QIANWEN_TTS_TEMP_DIRECTORY`;
- Aliyun OSS: `OSS_ENABLED`, `OBJECT_STORAGE_PROVIDER`, `OSS_REGION`,
  `OSS_ENDPOINT`, `OSS_BUCKET`, `OSS_AUDIO_PREFIX`, `OSS_IMAGE_PREFIX`,
  `OSS_RESUME_PREFIX`, `OSS_SIGNED_URL_TTL`, and
  `OSS_CREDENTIALS_PROVIDER`;
- OSS environment credentials: `OSS_ACCESS_KEY_ID`,
  `OSS_ACCESS_KEY_SECRET`, and optional `OSS_SESSION_TOKEN`, with
  `OSS_RAM_ROLE_NAME` empty; or a verified Alibaba ECS role source with the
  environment credentials absent and `OSS_RAM_ROLE_NAME` recorded;
- XFYun ISE: `XFYUN_ISE_ENDPOINT`, `XFYUN_ISE_TIMEOUT`, `APPID`, `APIKey`, and
  `APISecret` from one domestic application and service entitlement.

- [ ] The private provider matrix records, for every field above, the Singapore
  baseline, China candidate, resource region and owner, secret classification,
  validation receipt, and rollback value or environment hash.
- [ ] `TEXT_GENERATION_PROVIDER`, `SPEECH_RECOGNITION_PROVIDER`, and
  `SPEECH_SYNTHESIS_PROVIDER` are all `qianwen`; `OSS_ENABLED=1` and
  `OBJECT_STORAGE_PROVIDER=aliyun_oss`.
- [ ] `AGENT_CONTEXT_MAX_CHARACTERS`, `AGENT_RUN_LOOP_TIMEOUT`, all `VOICE_*`
  limits, and other non-regional runtime values either match the tested
  Singapore baseline or have a separately reviewed reason to change.
- [ ] Avatar fields (`SPATIUS_ENABLED`, `SPATIUS_REGION`,
  `SPATIUS_CONSOLE_BASE_URL`, `SPATIUS_APP_ID`, `SPATIUS_AVATAR_ID`,
  `SPATIUS_API_KEY`, `SPATIUS_TOKEN_TTL`, `SPATIUS_TIMEOUT`) and OCR fields
  (`RESUME_OCR_ENABLED`, `PADDLEOCR_ACCESS_TOKEN`, `PADDLEOCR_BASE_URL`,
  `RESUME_OCR_TIMEOUT`) are each classified as `china`, `unchanged`, or
  `disabled`; enabled unchanged services passed China-host latency checks.
- [ ] No partial candidate file is accepted: a missing selector, model,
  timeout, endpoint, resource identifier, or required credential rejects the
  candidate from approval and keeps the China stack isolated from Production
  traffic; successful Server startup alone does not complete this matrix.

## 2. HTTPS, WSS, and App entry

- [ ] A trusted HTTPS edge terminates a valid certificate and proxies HTTP plus
  WebSocket upgrades to the loopback-only Server API.
- [ ] `/health` and `/readyz` pass through the intended public edge without
  exposing `/metrics`, PostgreSQL, or a container port.
- [ ] DNS TTL and the old `api.speak-up.top` origin are recorded before change.
- [ ] External probes confirm the China edge from at least two networks.

The Android Production flavor is compiled for `https://api.speak-up.top` and
rejects non-loopback HTTP/WS. Keeping that hostname allows an origin/DNS switch
without a new APK. Changing the API hostname requires a separately reviewed,
signed, higher-version APK. A public IP and plaintext high port are never a
Production entry.

## 3. Data and OSS migration

- [ ] A maintenance window and an explicit write-stop point are approved;
  there is no dual-write or replication contract today.
- [ ] Singapore PostgreSQL produced a final consistency backup with backup ID,
  metadata, size, and SHA-256.
- [ ] The final backup passed an isolated restore check before transfer.
- [ ] The encrypted transfer completed and the China restore passed schema,
  row-count, ownership, and application-readiness checks.
- [ ] Singapore OSS objects were copied to the China bucket and an inventory
  compared object keys, sizes, and checksums or ETags where their semantics are
  equivalent.
- [ ] The Singapore database and bucket remain intact and read-only throughout
  the rollback window.

Record `backup.json` and its SHA-256. Do not treat “the dump file exists” as a
restore test.

## 4. Acceptance matrix

- [ ] Five or more comparable runs cover login, authenticated HTTP, WebSocket,
  text response, voice upload, ASR, LLM, TTS, XFYun ISE, OSS upload and signed
  download, and Part 1/2/3/full report generation.
- [ ] Each scenario records attempts, successes, failure categories, P50, P95,
  and maximum duration for Singapore and China under the same client method.
- [ ] Android real-device testing used the reviewed APK; the SSH-tunnel debug
  APK was used only before the trusted HTTPS edge existed.
- [ ] `uat.json` names the release, endpoint, device build, provider region,
  and receipt SHA-256 without including user content or tokens.

## 5. Monitoring, alerting, and backups

- [ ] Local metrics are collected without public exposure; external probes
  monitor the public API independently of the China host.
- [ ] Dashboards cover HTTP success/5xx/P95, report success and duration,
  Qianwen/ASR/TTS/ISE/OSS calls and errors, container health, CPU, memory, disk,
  inode, PostgreSQL readiness, and certificate expiry.
- [ ] An alert drill reached the intended recipient and its evidence is
  recorded.
- [ ] Daily PostgreSQL backup, pre-cutover backup, retention, off-host copy,
  freshness check, and scheduled isolated restore are active.
- [ ] Docker and edge logs rotate within a measured disk budget.

## 6. GitHub and host control plane

- [ ] A dedicated protected GitHub Environment holds only China deployment
  credentials and requires the chosen human approval policy.
- [ ] The Environment permits only reviewed official `main` artifacts and pins
  SSH known-host identity.
- [ ] A non-root deployment user can invoke only the reviewed deployment entry;
  routine deployment does not use root password authentication.
- [ ] Deployment is locked to one operation and writes a content-addressed
  receipt. Re-running the same receipt is idempotent.

These controls are not required to finish the current Singapore release, but
they are required before repeatable China Production deployment.

## 7. Cutover

- [ ] `preflight.json`, `backup.json`, and `uat.json` all exist and their hashes
  match this plan.
- [ ] Writes are stopped at the recorded timestamp and the final database/OSS
  delta is zero.
- [ ] China Server starts from the pinned digest and candidate `server.env`;
  migrations and readiness succeed before traffic changes.
- [ ] The HTTPS origin or DNS changes to China and external health, login,
  WebSocket, voice, report, and download checks pass.
- [ ] Writes are re-enabled only after those checks pass.
- [ ] `cutover.json` records the approver, timestamps, old/new origin, digests,
  backup ID, checks, and final result.

## 8. Rollback

Rollback trigger examples must be assigned measured thresholds before cutover:
health/readiness failure, elevated 5xx or P95, provider failure, report failure,
or data-integrity mismatch.

- [ ] Stop new writes and preserve China logs, receipts, and the current
  database before changing traffic.
- [ ] If China accepted **no writes**, restore the recorded Singapore origin and
  verify the preserved Singapore database and provider configuration.
- [ ] If China accepted **any writes**, do not point traffic at the stale
  Singapore database. First back up China, restore or reconcile that state into
  the selected rollback database, verify it in isolation, and only then switch
  traffic.
- [ ] Restore the previous Server digest, environment hash, edge origin, and
  DNS value as one reviewed rollback operation.
- [ ] Re-run external health, login, WebSocket, voice, report, and object access
  checks before reopening writes.
- [ ] `rollback.json` records trigger, data boundary, target backup/digest,
  approver, checks, and result; its SHA-256 is attached to the incident record.

Until both the no-write and post-write rollback paths have been rehearsed, the
China stack remains an experiment and must not take Production traffic.
