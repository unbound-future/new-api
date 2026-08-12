#!/usr/bin/env bash
set -euo pipefail

DEPLOY_DIR=${DEPLOY_DIR:-/opt/newapi}
ENV_FILE=${ENV_FILE:-/etc/newapi/newapi.env}
DATA_DEVICE=${DATA_DEVICE:-/dev/vdb}
DATA_MOUNT=${DATA_MOUNT:-/data}

if [[ $EUID -ne 0 ]]; then
  echo "bootstrap-app.sh must run as root" >&2
  exit 1
fi

install_docker() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    return
  fi
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io docker-compose-v2 ca-certificates curl
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y docker docker-compose-plugin ca-certificates curl
  elif command -v yum >/dev/null 2>&1; then
    yum install -y docker docker-compose-plugin ca-certificates curl
  else
    echo "unsupported Linux distribution" >&2
    exit 1
  fi
  systemctl enable --now docker
}

prepare_data_disk() {
  mkdir -p "$DATA_MOUNT"
  if mountpoint -q "$DATA_MOUNT"; then
    return
  fi
  if [[ ! -b $DATA_DEVICE ]]; then
    echo "data device $DATA_DEVICE does not exist" >&2
    exit 1
  fi
  local fs_type
  fs_type=$(blkid -s TYPE -o value "$DATA_DEVICE" || true)
  if [[ -z $fs_type ]]; then
    mkfs.xfs -f "$DATA_DEVICE"
  fi
  local uuid
  uuid=$(blkid -s UUID -o value "$DATA_DEVICE")
  if ! grep -q "UUID=$uuid" /etc/fstab; then
    echo "UUID=$uuid $DATA_MOUNT xfs defaults,nofail 0 2" >> /etc/fstab
  fi
  mount "$DATA_MOUNT"
}

if [[ ! -f $ENV_FILE ]]; then
  echo "missing $ENV_FILE" >&2
  exit 1
fi
chmod 600 "$ENV_FILE"

install_docker
prepare_data_disk
install -d -m 0755 "$DEPLOY_DIR" "$DATA_MOUNT/newapi/coslog" "$DATA_MOUNT/newapi/logs"

if [[ ! -f $DEPLOY_DIR/compose.yaml ]]; then
  echo "missing $DEPLOY_DIR/compose.yaml" >&2
  exit 1
fi

cd "$DEPLOY_DIR"
docker compose --env-file "$ENV_FILE" -f compose.yaml config --quiet
docker compose --env-file "$ENV_FILE" -f compose.yaml pull
docker compose --env-file "$ENV_FILE" -f compose.yaml up -d --remove-orphans
