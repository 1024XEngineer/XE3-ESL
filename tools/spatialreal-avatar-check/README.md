# SpatialReal avatar compatibility check

This tool checks whether SpatialReal character metadata satisfies the resource
shape required by the Android AvatarKit version currently used by SpeakUp. It
does not read an API key and does not create an avatar session.

Check the avatars configured in `avatars.json`:

```bash
make check-spatialreal-avatars
```

Check one or more IDs copied from SpatialReal Studio:

```bash
node tools/spatialreal-avatar-check/check.mjs \
  --avatar Lisa=94a60c13-e835-4bde-aa93-00a1cf178dcd \
  --avatar Nathan=1843ff9f-db3a-45de-be28-9c2b9d6412a3
```

Add `--probe-assets` to send a `HEAD` request to every referenced CDN asset,
or `--json` for machine-readable output. The command exits with status `1` if
any avatar is incompatible or unavailable, and status `2` for invalid usage.

Passing this preflight means that the metadata and optional CDN resources are
compatible. It does not replace a physical-device rendering test, which also
depends on session authorization, device support, networking, and the native
renderer.
