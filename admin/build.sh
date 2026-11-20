#!/usr/bin/env sh
set -eu
admin_root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
make -C "$admin_root" build_only
