#!/usr/bin/env bash
set -euo pipefail

DEPLOY_DIR=${DEPLOY_DIR:-/opt/newapi}
ENV_FILE=${ENV_FILE:-/etc/newapi/newapi.env}
cd "$DEPLOY_DIR"

docker compose --env-file "$ENV_FILE" -f compose-data.yaml ps
curl --fail --silent --show-error --output /dev/null http://127.0.0.1/api/status
docker exec newapi-postgres pg_isready -U newapi -d newapi
docker exec newapi-redis sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli ping' | grep -q PONG
docker exec newapi-clickhouse sh -c 'clickhouse-client --user "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" --query "SELECT count() FROM newapi_logs.logs"'
docker inspect --format '{{.Config.Image}} {{.State.Status}} {{.State.Health.Status}}' new-api
df -h /data
