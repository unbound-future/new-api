#!/usr/bin/env bash
set -euo pipefail

DEPLOY_DIR=${DEPLOY_DIR:-/opt/newapi}
ENV_FILE=${ENV_FILE:-/etc/newapi/newapi.env}
COMPOSE_FILE=${COMPOSE_FILE:-compose-app.yaml}
cd "$DEPLOY_DIR"

docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" ps
curl --fail --silent --show-error --output /dev/null http://127.0.0.1/api/status
docker inspect --format '{{.Config.Image}} {{.State.Status}} {{.State.Health.Status}}' new-api
docker inspect --format '{{.State.Status}} {{.State.Health.Status}}' newapi-nginx
df -h /data
