# COSLOG and Request Log Switches Design

## Goal

Add independent, backward-compatible runtime switches for COSLOG and the custom MySQL `request_logs` writer, while preserving billing, quota updates, ordinary consumption logs, login, log-list queries, and access to historical request-log details.

## Confirmed production behavior

The first production rollout will set `COSLOG_ENABLED=false` and leave `REQUEST_LOG_ENABLED=true`. This stops local JSONL/GCS logging but intentionally keeps MySQL `request_logs` writes until a later, separately approved rollout.

## Configuration contract

| Variable | Default | Effect when `false` |
| --- | --- | --- |
| `COSLOG_ENABLED` | `false` (existing behavior) | Do not initialize the COSLOG writer, copy request bodies for COSLOG, enqueue entries, write local JSONL files, or upload them to GCS. |
| `REQUEST_LOG_ENABLED` | `true` | Do not insert or update `request_logs`. Historical reads remain available. |

The two switches are independent:

| COSLOG | request_logs | Response capture |
| --- | --- | --- |
| on | on | Enabled for both consumers. |
| off | on | Enabled only for MySQL `request_logs`. This is the first production rollout. |
| on | off | Enabled only for COSLOG. |
| off | off | Bypassed completely; no full response buffering. |

## Architecture

1. Load both environment switches once during common application initialization.
2. Make COSLOG preparation and recording no-ops when COSLOG is disabled, so disabled COSLOG cannot retain request bodies or create queue pressure.
3. Guard both request-log creation and response flushing with `REQUEST_LOG_ENABLED`.
4. Make `ResponseCaptureMiddleware` pass through without wrapping the response writer only when both consumers are disabled. If either consumer is enabled, retain the current capture behavior.
5. Keep `RequestLog` schema migration and read APIs unchanged so disabling writes does not remove or hide historical data.

## Files expected to change

- `common/var.go`: declare the two runtime flags.
- `common/init.go`: load `COSLOG_ENABLED` and `REQUEST_LOG_ENABLED` from the environment.
- `pkg/coslog/config.go`: use the common COSLOG flag consistently.
- `pkg/coslog/log_entry.go`: skip request preparation and recording when disabled.
- `model/request_log.go`: skip create/update operations when request logging is disabled.
- `middleware/response_capture.go`: bypass response buffering when both consumers are disabled.
- Focused Go test files beside the affected packages.
- `/root/tunnel-deploy/newapi/docker-compose.yml`: first rollout sets `COSLOG_ENABLED=false` and explicitly sets `REQUEST_LOG_ENABLED=true` only after the new image passes validation.

## Safety and compatibility

- No database migration, table deletion, connection-pool change, or historical-data cleanup.
- `logs`, billing, quota updates, batch updates, authentication, and log-list queries remain unchanged.
- Existing behavior is preserved when `REQUEST_LOG_ENABLED` is unset because it defaults to `true`.
- Sensitive request headers are not printed during tests or deployment checks.
- The current image `new-api:b4892b87` and the verified backup remain the rollback target.

## Test strategy

Tests will be written before implementation and must demonstrate:

1. `REQUEST_LOG_ENABLED` defaults to `true` and parses explicit `false`.
2. Disabled COSLOG does not prepare or enqueue request content.
3. Disabled request logging performs no `request_logs` create/update writes.
4. Response capture is bypassed only when both consumers are disabled.
5. Response capture remains active for each single-consumer combination.
6. Existing package tests and a production build complete successfully.

## Deployment and rollback

1. Build a new binary and a uniquely tagged Docker image; do not overwrite `new-api:b4892b87`.
2. Start a green container with a complete copy of the current environment, networks, mounts, and dependencies.
3. For the first rollout use `COSLOG_ENABLED=false` and `REQUEST_LOG_ENABLED=true`.
4. Verify health, login, log list/detail, one model request, quota settlement, and continued `request_logs` growth.
5. Switch Nginx to green with a graceful reload and leave blue available for rollback.
6. Observe errors, memory, MySQL connections/transactions, and request latency. On regression, switch Nginx back to the old container and reload.

Updating code therefore requires compiling the binary, building a new image tag, and recreating or starting a container from that image. Merely changing an image tag does not alter an already-running container.

## Success criteria

- With the first-rollout settings, no new COSLOG local files or GCS queue activity occurs.
- MySQL `request_logs` continues to write because it remains enabled.
- Ordinary business functions and historical log reads continue to work.
- The green deployment can be rolled back without database changes.
