#!/bin/sh

set -eu

: "${CERTBOT_EMAIL:?Set CERTBOT_EMAIL to the certificate contact address}"

portal_dir="${XE3_PORTAL_DIR:-/opt/xe3-speakup-portal}"
certbot_conf="${portal_dir}/certbot/conf"
certbot_webroot="${portal_dir}/certbot/www"

mkdir -p "${certbot_conf}" "${certbot_webroot}"

/usr/bin/docker run --rm \
  --name xe3-certbot-issue \
  -v "${certbot_conf}:/etc/letsencrypt" \
  -v "${certbot_webroot}:/var/www/certbot" \
  certbot/certbot certonly \
  --webroot \
  --webroot-path /var/www/certbot \
  --cert-name speak-up.top \
  --domain speak-up.top \
  --domain www.speak-up.top \
  --email "${CERTBOT_EMAIL}" \
  --agree-tos \
  --no-eff-email \
  --non-interactive
