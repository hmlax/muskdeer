#!/bin/sh
set -eu

# The image kind is fixed at build time; container environment cannot switch portals.
ADMIN_APP=$(cat /etc/admin-app)
case "$ADMIN_APP" in
  platform) MERCHANT_CODE="" ;;
  merchant)
    case "${MERCHANT_CODE:-}" in
      ''|*[!A-Za-z0-9_-]*) echo 'Merchant admin requires MERCHANT_CODE (letters, numbers, underscore or hyphen).' >&2; exit 1 ;;
    esac
    if [ "${#MERCHANT_CODE}" -gt 128 ]; then
      echo 'MERCHANT_CODE is too long.' >&2
      exit 1
    fi
    ;;
  *) echo 'ADMIN_APP must be platform or merchant.' >&2; exit 1 ;;
esac

export MERCHANT_CODE
printf '{"merchantCode":"%s"}\n' "$MERCHANT_CODE" > /usr/share/nginx/html/app-config.json
