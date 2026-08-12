#!/usr/bin/env sh
set -eu

: "${CLICKHOUSE_CLUSTER:?CLICKHOUSE_CLUSTER is required}"

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEMPLATE="$SCRIPT_DIR/../clickhouse/schema.sql.tpl"
OUTPUT=${1:-"$SCRIPT_DIR/../clickhouse/schema.rendered.sql"}

case "$CLICKHOUSE_CLUSTER" in
  *[!A-Za-z0-9_.-]*)
    echo "CLICKHOUSE_CLUSTER contains unsupported characters" >&2
    exit 1
    ;;
esac

sed "s/\${CLICKHOUSE_CLUSTER}/$CLICKHOUSE_CLUSTER/g" "$TEMPLATE" > "$OUTPUT"
chmod 600 "$OUTPUT"
echo "$OUTPUT"
