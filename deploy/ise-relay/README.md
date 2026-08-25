# SpeakUp ISE Relay

This isolated service keeps iFlytek ISE credentials in mainland China and
accepts evaluation requests only from the SpeakUp Staging backend over mTLS.
It stores PCM input in memory only and does not expose provider raw responses.

## Trust boundary

- Publish TCP `18443`; restrict the Tencent Cloud security group source to
  `149.71.241.71/32`.
- Use a dedicated private CA and client certificate for the Staging backend.
- Include `122.51.24.153` as an IP SAN in the relay server certificate.
- Do not publish the container-internal `18080` health and metrics port.
- Keep the populated environment file and private keys outside the repository.
- Do not configure both direct XFYUN credentials and `ISE_RELAY_*` client
  settings in the Staging backend. Startup fails closed if both are present.

## Runtime files

```text
/opt/speakup-ise-relay/
├── deploy.env
├── relay.env
└── secrets/
    ├── client-ca.pem
    ├── server-key.pem
    └── server.pem
```

The secrets directory must be readable by container UID `10001` and must not
contain the Staging client private key. The client certificate, client key and
CA certificate belong on the Singapore server.

## Deploy and verify

```bash
docker compose --env-file /opt/speakup-ise-relay/deploy.env \
  -f deploy/ise-relay/compose.yaml config --quiet
docker compose --env-file /opt/speakup-ise-relay/deploy.env \
  -f deploy/ise-relay/compose.yaml up -d
docker compose --env-file /opt/speakup-ise-relay/deploy.env \
  -f deploy/ise-relay/compose.yaml ps
```

Verify the public endpoint from the Singapore server with its mTLS client
certificate. A request without a trusted client certificate must fail during
the TLS handshake.
