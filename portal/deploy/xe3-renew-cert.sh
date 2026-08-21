#!/bin/sh

set -eu

/usr/bin/docker run --rm \
  --name xe3-certbot-renew \
  -v /opt/xe3-speakup-portal/certbot/conf:/etc/letsencrypt \
  -v /opt/xe3-speakup-portal/certbot/www:/var/www/certbot \
  certbot/certbot renew \
  --webroot \
  --webroot-path /var/www/certbot \
  --quiet

/usr/local/nginx/sbin/nginx -t
/usr/local/nginx/sbin/nginx -s reload
