#!/usr/bin/env bash
set -eu

metadata() {
  curl -fsS -H 'Metadata-Flavor: Google' "http://metadata.google.internal/computeMetadata/v1/$1"
}

PROJECT_ID="$(metadata project/project-id)"
MYSQL_HOST="$(metadata instance/attributes/newapi-mysql-host)"
REDIS_HOST="$(metadata instance/attributes/newapi-redis-host)"
IMAGE="$(metadata instance/attributes/newapi-image)"
BUCKET="$(metadata instance/attributes/newapi-coslog-bucket)"
PREFIX="$(metadata instance/attributes/newapi-coslog-prefix)"

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl docker.io
systemctl enable --now docker

if ! systemctl list-unit-files google-cloud-ops-agent.service >/dev/null 2>&1; then
  curl -fsSLo /tmp/add-google-cloud-ops-agent-repo.sh https://dl.google.com/cloudagents/add-google-cloud-ops-agent-repo.sh
  bash /tmp/add-google-cloud-ops-agent-repo.sh --also-install
fi
systemctl enable --now google-cloud-ops-agent

MYSQL_PASSWORD="$(gcloud secrets versions access latest --secret=newapi-mysql-root-password --project="$PROJECT_ID")"
SESSION_SECRET="$(gcloud secrets versions access latest --secret=newapi-session-secret --project="$PROJECT_ID")"
CRYPTO_SECRET="$(gcloud secrets versions access latest --secret=newapi-crypto-secret --project="$PROJECT_ID")"

install -d -m 0755 /etc/newapi /var/lib/newapi/oss_log
install -m 0600 /dev/null /etc/newapi/newapi.env
printf '%s\n' \
  "TZ=Asia/Shanghai" \
  "PORT=3000" \
  "SQL_DSN=root:${MYSQL_PASSWORD}@tcp(${MYSQL_HOST}:3306)/newapi?charset=utf8mb4&parseTime=True&loc=Local" \
  "SQL_MAX_OPEN_CONNS=1000" \
  "SQL_MAX_IDLE_CONNS=100" \
  "SQL_MAX_LIFETIME=60" \
  "REDIS_CONN_STRING=redis://${REDIS_HOST}:6379" \
  "SESSION_SECRET=${SESSION_SECRET}" \
  "CRYPTO_SECRET=${CRYPTO_SECRET}" \
  "BATCH_UPDATE_ENABLED=true" \
  "BATCH_UPDATE_INTERVAL=5" \
  "REQUEST_LOG_ENABLED=false" \
  "COSLOG_ENABLED=true" \
  "COSLOG_TRANSPORT=pubsub" \
  "COSLOG_STORAGE_TYPE=gcs" \
  "COSLOG_LOCAL_DIR=/data/oss_log" \
  "COSLOG_DELETE_AFTER_UPLOAD=true" \
  "COSLOG_FLUSH_SIZE=10000" \
  "COSLOG_FLUSH_INTERVAL=120" \
  "COSLOG_MAX_FILE_SIZE=104857600" \
  "COSLOG_PUBSUB_PROJECT_ID=${PROJECT_ID}" \
  "COSLOG_PUBSUB_TOPIC=newapi-coslog" \
  "COSLOG_PUBSUB_SUBSCRIPTION=newapi-coslog-gcs" \
  "COSLOG_PUBSUB_MAX_MESSAGE_BYTES=9000000" \
  "COSLOG_PUBSUB_PUBLISH_WORKERS=32" \
  "OSS_BUCKET=${BUCKET}" \
  "OSS_PREFIX=${PREFIX}" \
  > /etc/newapi/newapi.env
unset MYSQL_PASSWORD SESSION_SECRET CRYPTO_SECRET

gcloud auth print-access-token --project="$PROJECT_ID" \
  | docker login -u oauth2accesstoken --password-stdin https://us-east4-docker.pkg.dev
docker pull "$IMAGE"
docker rm -f newapi >/dev/null 2>&1 || true
docker run -d \
  --name newapi \
  --restart unless-stopped \
  --env-file /etc/newapi/newapi.env \
  -p 3000:3000 \
  -v /var/lib/newapi:/data \
  "$IMAGE"
