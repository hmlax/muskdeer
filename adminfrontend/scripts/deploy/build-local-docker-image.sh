#!/bin/bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
FRONTEND_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd)
ADMIN_APP=${ADMIN_APP:-platform}

case "$ADMIN_APP" in
  platform|merchant) ;;
  *) echo 'ADMIN_APP must be platform or merchant.' >&2; exit 1 ;;
esac

exec docker build "$FRONTEND_ROOT" -f "$FRONTEND_ROOT/Dockerfile" \
  --build-arg "ADMIN_APP=$ADMIN_APP" -t "go-admin/$ADMIN_APP-admin:local"
