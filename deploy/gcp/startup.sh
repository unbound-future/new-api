#!/usr/bin/env bash
set -eu

metadata() {
  curl -fsS -H 'Metadata-Flavor: Google' "http://metadata.google.internal/computeMetadata/v1/$1"
}

PROJECT_ID="$(metadata project/project-id)"
IMAGE="$(metadata instance/attributes/newapi-image)"
CONFIG_URI="$(metadata instance/attributes/newapi-config-uri)"

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl docker.io
systemctl enable --now docker

if ! systemctl list-unit-files google-cloud-ops-agent.service >/dev/null 2>&1; then
  curl -fsSLo /tmp/add-google-cloud-ops-agent-repo.sh https://dl.google.com/cloudagents/add-google-cloud-ops-agent-repo.sh
  bash /tmp/add-google-cloud-ops-agent-repo.sh --also-install
fi
systemctl enable --now google-cloud-ops-agent

install -d -m 0755 /etc/newapi /var/lib/newapi/oss_log
gcloud storage cp "$CONFIG_URI" /etc/newapi/newapi.env --project="$PROJECT_ID"
chmod 0600 /etc/newapi/newapi.env

for required_variable in SQL_DSN REDIS_CONN_STRING SESSION_SECRET CRYPTO_SECRET; do
  if ! grep -q "^${required_variable}=" /etc/newapi/newapi.env; then
    echo "missing required variable in private configuration: ${required_variable}" >&2
    exit 1
  fi
done

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
