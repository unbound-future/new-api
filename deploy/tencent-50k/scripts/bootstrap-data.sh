#!/usr/bin/env bash
set -euo pipefail

DEPLOY_DIR=${DEPLOY_DIR:-/opt/newapi}
ENV_FILE=${ENV_FILE:-/etc/newapi/newapi.env}
DATA_DEVICE=${DATA_DEVICE:-/dev/vdb}
DATA_MOUNT=${DATA_MOUNT:-/data}

if [[ $EUID -ne 0 ]]; then
  echo "bootstrap-data.sh must run as root" >&2
  exit 1
fi

install_runtime() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    return
  fi
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io docker-compose-v2 ca-certificates curl xfsprogs
  systemctl enable --now docker
}

configure_registry_access() {
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
}

prepare_data_disk() {
  install -d -m 0755 "$DATA_MOUNT"
  if ! mountpoint -q "$DATA_MOUNT"; then
    [[ -b $DATA_DEVICE ]] || { echo "missing data device $DATA_DEVICE" >&2; exit 1; }
    local fs_type uuid
    fs_type=$(blkid -s TYPE -o value "$DATA_DEVICE" || true)
    if [[ -z $fs_type ]]; then
      mkfs.xfs -f "$DATA_DEVICE"
    elif [[ $fs_type != xfs ]]; then
      echo "$DATA_DEVICE already contains unsupported filesystem $fs_type" >&2
      exit 1
    fi
    uuid=$(blkid -s UUID -o value "$DATA_DEVICE")
    grep -q "UUID=$uuid" /etc/fstab || echo "UUID=$uuid $DATA_MOUNT xfs defaults,nofail 0 2" >> /etc/fstab
    mount "$DATA_MOUNT"
  fi
}

[[ -f $ENV_FILE ]] || { echo "missing $ENV_FILE" >&2; exit 1; }
[[ -f $DEPLOY_DIR/compose-data.yaml ]] || { echo "missing compose-data.yaml" >&2; exit 1; }
chmod 600 "$ENV_FILE"

install_runtime
configure_registry_access
prepare_data_disk
install -d -m 0700 -o 999 -g 999 "$DATA_MOUNT/postgres" "$DATA_MOUNT/redis"
install -d -m 0700 -o 101 -g 101 "$DATA_MOUNT/clickhouse"
install -d -m 0755 "$DATA_MOUNT/newapi/coslog" "$DATA_MOUNT/newapi/logs"

cd "$DEPLOY_DIR"
docker compose --env-file "$ENV_FILE" -f compose-data.yaml config --quiet
docker compose --env-file "$ENV_FILE" -f compose-data.yaml pull
docker compose --env-file "$ENV_FILE" -f compose-data.yaml up -d --remove-orphans
