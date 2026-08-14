#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEMPLATE="$SCRIPT_DIR/../clickhouse/schema.sql.tpl"
OUTPUT=${1:-"$SCRIPT_DIR/../clickhouse/schema.rendered.sql"}

install -m 600 "$TEMPLATE" "$OUTPUT"
echo "$OUTPUT"
