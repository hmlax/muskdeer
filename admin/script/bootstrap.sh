#!/usr/bin/env sh
set -eu
admin_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$admin_root"
exec ./bin/admin
