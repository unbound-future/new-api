#!/usr/bin/env bash
set -euo pipefail

DEPLOY_DIR=${DEPLOY_DIR:-/opt/newapi}
ENV_FILE=${ENV_FILE:-/etc/newapi/newapi.env}
cd "$DEPLOY_DIR"

docker compose --env-file "$ENV_FILE" -f compose.yaml ps
curl --fail --silent --show-error http://127.0.0.1:3000/api/status
docker inspect --format '{{.Config.Image}} {{.State.Status}} {{.State.Health.Status}}' new-api
df -h /data
