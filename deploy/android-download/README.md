# Android public download publication

This directory contains the host-side contract for publishing the already
signed Production APK. It does not build or sign an APK, change Nginx, deploy
containers, or infer a publication time.

## Inputs and public layout

Use the strict v1 `release-manifest.json` and the Production APK from the same
successful Release Candidate run. The bundle builder rejects a mismatched
filename, size, SHA-256, package contract, minimum API, ABI, certificate
fingerprint, or manifest schema. The operator must also supply a real canonical
RFC3339 UTC time without fractional seconds.

```sh
node tools/android-download/bundle.mjs \
  --manifest /opt/speakup/releases/v0.1.1/release-manifest.json \
  --production-apk \
    /opt/speakup/releases/v0.1.1/speakup-v0.1.1-production-arm64.apk \
  --published-at 2026-08-23T12:34:56Z \
  --output /opt/speakup/releases/v0.1.1/android-download-bundle
```

The output is deterministic for identical inputs and contains only:

```text
bundle-manifest.json
downloads/android/v0.1.1/release.json
downloads/android/v0.1.1/speakup-v0.1.1-production-arm64.apk
downloads/android/v0.1.1/speakup-v0.1.1-production-arm64.apk.sha256
```

The builder creates a new output directory atomically and refuses to replace an
existing path. Record the printed bundle manifest SHA-256 in the release
record. Transfer the whole bundle without editing it.

## Prepare a host public root

The publisher must run as the owner of the public root. The root and any
existing `downloads` and `downloads/android` directories must be real
directories owned by the current UID and must not be group- or world-writable.
New publication directories are mode `0755`; public files are mode `0644`.

```sh
install -d -m 0755 /var/www/speakup-staging-public
install -d -m 0755 /var/www/speakup-production-public
```

Configure the corresponding absolute path as `STAGING_PUBLIC_ROOT` or
`PRODUCTION_PUBLIC_ROOT`. Nginx serves these files; the Portal container does
not contain the APK.

## Validate, publish, and activate

First validate the complete bundle and destination without changing the
destination:

```sh
./deploy/android-download/manage.sh validate \
  --bundle /opt/speakup/releases/v0.1.1/android-download-bundle \
  --root /var/www/speakup-staging-public
```

Publish reserves a new version directory and never overwrites an existing one.
Omit `--activate` until the versioned URLs have been verified:

```sh
./deploy/android-download/manage.sh publish \
  --bundle /opt/speakup/releases/v0.1.1/android-download-bundle \
  --root /var/www/speakup-staging-public

./deploy/android-download/manage.sh activate \
  --root /var/www/speakup-staging-public \
  --version 0.1.1
```

Run the same validate/publish sequence with the exact same bundle in
Production only after Staging acceptance and the separate Production approval.
The Production validation output must repeat the bundle manifest SHA-256
recorded during Staging; stop if it differs.
Publishing a version does not change the public Portal link. Activation copies
that version's validated metadata to `downloads/android/release.json` through a
same-directory atomic rename; the versioned APK and metadata remain immutable.

The Portal hostname exposes only the following Android paths:

- `/downloads/android/release.json`: current metadata, `Cache-Control: no-store`;
- the exact versioned `release.json`, APK, and `.sha256`: one-year immutable
  cache;
- every directory, unknown filename, mismatched version, and `latest.apk`:
  `404`.

Staging applies its existing HTTP Basic Authentication to all of these routes.
The API host always returns `404` for the Android download namespace.

## Roll back the public link

Reactivate any previously published and still-valid version:

```sh
./deploy/android-download/manage.sh activate \
  --root /var/www/speakup-production-public \
  --version 0.1.0
```

This atomically restores the previous metadata link without deleting either
version. It cannot downgrade devices that already installed a higher
`versionCode`; those devices require a newly signed hotfix with a higher code.

## Reproducible checks

```sh
make check-android-download
make check-staging-nginx
make check-production-nginx
```

The checks cover valid publication and reactivation plus malformed manifests,
tampering, unsafe roots, symlinks, no-clobber behavior, Nginx route exposure,
cache headers, MIME type, Basic Auth, and API denial.
