#!/usr/bin/env bash
set -euo pipefail

DEPLOY_DIR=${DEPLOY_DIR:-/opt/newapi}
ENV_FILE=${ENV_FILE:-/etc/newapi/newapi.env}

if [[ $EUID -ne 0 ]]; then
  echo "bootstrap-app.sh must run as root" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1 || ! docker compose version >/dev/null 2>&1; then
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io docker-compose-v2 ca-certificates curl
  systemctl enable --now docker
fi

if [[ ! -s /etc/docker/daemon.json ]]; then
  printf '%s\n' '{"registry-mirrors":["https://mirror.ccs.tencentyun.com"]}' > /etc/docker/daemon.json
  systemctl restart docker
fi
if [[ -f /root/newapi-tcr-login.env ]]; then
  set -a
  # shellcheck disable=SC1091
  source /root/newapi-tcr-login.env
  set +a
  printf '%s' "$TCR_PASSWORD" | docker login "$TCR_REGISTRY" -u "$TCR_USERNAME" --password-stdin >/dev/null
fi

[[ -f $ENV_FILE ]] || { echo "missing $ENV_FILE" >&2; exit 1; }
[[ -f $DEPLOY_DIR/compose-app.yaml ]] || { echo "missing compose-app.yaml" >&2; exit 1; }
chmod 600 "$ENV_FILE"
install -d -m 0755 /data/newapi/coslog /data/newapi/logs

cd "$DEPLOY_DIR"
docker compose --env-file "$ENV_FILE" -f compose-app.yaml config --quiet
docker compose --env-file "$ENV_FILE" -f compose-app.yaml pull
docker compose --env-file "$ENV_FILE" -f compose-app.yaml up -d --remove-orphans
