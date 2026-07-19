# GCP production deployment

This directory contains the startup script used by the `newapi-prod` regional
managed instance group. Runtime secrets are read from Secret Manager and are
never stored in this repository or in instance-template metadata.

The template supplies only non-secret metadata: the immutable Artifact
Registry image digest, private MySQL and Redis addresses, and the existing GCS
bucket/prefix. The application listens on port 3000; firewall rules restrict
that port to Google load-balancer and health-check ranges.

COSLOG uses the shared `newapi-coslog-gcs` pull subscription. A message is
acknowledged only after its JSONL batch is uploaded to GCS. Pub/Sub is
at-least-once, so duplicate records are intentionally retained.
