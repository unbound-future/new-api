# Request Logging Switches Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add independent COSLOG and MySQL `request_logs` write switches, then deploy a new image with COSLOG disabled and request logging still enabled.

**Architecture:** Load both flags once in `common.InitEnv`. COSLOG preparation/recording and request-log writes return before touching payloads or storage when their consumer is disabled. Response capture remains active while either consumer needs full responses and becomes a pass-through only when both are disabled.

**Tech Stack:** Go 1.25+ module, Gin, GORM, Docker multi-stage builds, Docker bridge networking, Nginx graceful reload.

## Global Constraints

- `REQUEST_LOG_ENABLED` defaults to `true`; the first production rollout explicitly keeps it `true`.
- The first production rollout sets `COSLOG_ENABLED=false`.
- Do not change database schema, historical data, MySQL settings, application connection pools, billing, quota settlement, batch updates, authentication, or ordinary `logs` behavior.
- Preserve SQLite, MySQL, and PostgreSQL compatibility.
- Do not print DSNs, service-account JSON, API keys, request headers, or request/response bodies.
- Keep `new-api:b4892b87` running and available until the green image passes checks and traffic is switched.
- Perform all code work in `/root/project/new-api-worktrees/coslog-requestlog-switches` on branch `codex/coslog-requestlog-switches`.

---

### Task 1: Load backward-compatible logging flags

**Files:**
- Modify: `common/constants.go`
- Modify: `common/init.go`
- Create: `common/logging_flags_test.go`

**Interfaces:**
- Produces: `common.CosLogEnabled bool`, default `false`.
- Produces: `common.RequestLogEnabled bool`, default `true`.
- Produces: `initLoggingFlags()` called by `InitEnv()`.

- [ ] **Step 1: Run the focused baseline tests in an isolated Go container**

```bash
cd /root/project/new-api-worktrees/coslog-requestlog-switches
docker run --rm --cpus=2 --memory=4g \
  -e CGO_ENABLED=0 -e GOMAXPROCS=2 \
  -v "$PWD:/src" -w /src \
  golang:1.26.1-alpine \
  go test ./common ./middleware ./model ./pkg/coslog
```

Expected: existing focused tests pass. If they do not, stop and report the baseline failure before editing production code.

- [ ] **Step 2: Write the failing flag test**

```go
package common

import "testing"

func TestInitLoggingFlags(t *testing.T) {
	oldCosLogEnabled := CosLogEnabled
	oldRequestLogEnabled := RequestLogEnabled
	t.Cleanup(func() {
		CosLogEnabled = oldCosLogEnabled
		RequestLogEnabled = oldRequestLogEnabled
	})

	t.Run("defaults preserve existing behavior", func(t *testing.T) {
		t.Setenv("COSLOG_ENABLED", "")
		t.Setenv("REQUEST_LOG_ENABLED", "")
		initLoggingFlags()
		if CosLogEnabled {
			t.Fatal("COSLOG must default to disabled")
		}
		if !RequestLogEnabled {
			t.Fatal("request_logs must default to enabled")
		}
	})

	t.Run("explicit values are parsed independently", func(t *testing.T) {
		t.Setenv("COSLOG_ENABLED", "true")
		t.Setenv("REQUEST_LOG_ENABLED", "false")
		initLoggingFlags()
		if !CosLogEnabled || RequestLogEnabled {
			t.Fatalf("unexpected flags: coslog=%t request_log=%t", CosLogEnabled, RequestLogEnabled)
		}
	})
}
```

- [ ] **Step 3: Verify the test fails for the missing interface**

Run: `go test ./common -run TestInitLoggingFlags -count=1`

Expected: compile failure because the new flags/function do not exist.

- [ ] **Step 4: Add the minimal flag implementation**

Add to `common/constants.go`:

```go
var CosLogEnabled = false
var RequestLogEnabled = true
```

Add to `common/init.go` and call it from `InitEnv()`:

```go
func initLoggingFlags() {
	CosLogEnabled = GetEnvOrDefaultBool("COSLOG_ENABLED", false)
	RequestLogEnabled = GetEnvOrDefaultBool("REQUEST_LOG_ENABLED", true)
}
```

- [ ] **Step 5: Verify green and commit**

Run: `go test ./common -run TestInitLoggingFlags -count=1`

Expected: PASS.

```bash
git add common/constants.go common/init.go common/logging_flags_test.go
git commit -m "feat: add full request logging switches"
```

---

### Task 2: Make disabled COSLOG stop retaining payloads

**Files:**
- Modify: `pkg/coslog/config.go`
- Modify: `pkg/coslog/log_entry.go`
- Create: `pkg/coslog/log_entry_test.go`

**Interfaces:**
- Consumes: `common.CosLogEnabled`.
- Produces: disabled `PrepareContext` and `Record` paths that return before reading or retaining request/response payloads.

- [ ] **Step 1: Write the failing disabled-COSLOG test**

```go
package coslog

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func TestPrepareContextSkipsPayloadWhenDisabled(t *testing.T) {
	old := common.CosLogEnabled
	common.CosLogEnabled = false
	t.Cleanup(func() { common.CosLogEnabled = old })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"secret":"payload"}`))
	PrepareContext(c)

	if _, exists := c.Get(CtxKeyRequestBody); exists {
		t.Fatal("disabled COSLOG must not retain request body")
	}
	if _, exists := c.Get(CtxKeyRequestHeaders); exists {
		t.Fatal("disabled COSLOG must not retain request headers")
	}
}
```

- [ ] **Step 2: Verify red**

Run: `go test ./pkg/coslog -run TestPrepareContextSkipsPayloadWhenDisabled -count=1`

Expected: FAIL because the current implementation stores the body and headers.

- [ ] **Step 3: Implement minimal COSLOG guards**

In `pkg/coslog/config.go`, set `Config.Enabled` from `common.CosLogEnabled` instead of reparsing the environment.

At the beginning of `PrepareContext`:

```go
if !common.CosLogEnabled || ctx == nil {
	return
}
```

At the beginning of `Record`, extend the current guard:

```go
if !common.CosLogEnabled || defaultWriter == nil || ctx == nil {
	return
}
```

- [ ] **Step 4: Verify green and existing writer behavior**

Run: `go test ./pkg/coslog -count=1`

Expected: all COSLOG tests pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/coslog/config.go pkg/coslog/log_entry.go pkg/coslog/log_entry_test.go
git commit -m "fix: make disabled coslog skip payload capture"
```

---

### Task 3: Bypass response buffering only when both consumers are off

**Files:**
- Modify: `middleware/response_capture.go`
- Modify: `middleware/response_capture_test.go`

**Interfaces:**
- Consumes: `common.CosLogEnabled`, `common.RequestLogEnabled`.
- Produces: pass-through middleware when both flags are false; current capture behavior otherwise.

- [ ] **Step 1: Write the failing behavior-matrix test**

Add a table-driven test that sets both flags for all four combinations. Inside the handler, assert that `c.Writer` is a `*streamCaptureWriter` for `true/true`, `false/true`, and `true/false`, and is not a `*streamCaptureWriter` for `false/false`. Also update the existing capture test to explicitly set `RequestLogEnabled=true` and restore both globals after the test.

```go
func TestResponseCaptureMiddlewareFlagMatrix(t *testing.T) {
	tests := []struct {
		name       string
		coslog     bool
		requestLog bool
		captured   bool
	}{
		{"both enabled", true, true, true},
		{"request log only", false, true, true},
		{"coslog only", true, false, true},
		{"both disabled", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldCoslog, oldRequestLog := common.CosLogEnabled, common.RequestLogEnabled
			common.CosLogEnabled, common.RequestLogEnabled = tt.coslog, tt.requestLog
			t.Cleanup(func() {
				common.CosLogEnabled, common.RequestLogEnabled = oldCoslog, oldRequestLog
			})

			var captured bool
			router := gin.New()
			router.Use(ResponseCaptureMiddleware())
			router.GET("/", func(c *gin.Context) {
				_, captured = c.Writer.(*streamCaptureWriter)
				c.Status(http.StatusNoContent)
			})
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
			if captured != tt.captured {
				t.Fatalf("captured=%t, want %t", captured, tt.captured)
			}
		})
	}
}
```

- [ ] **Step 2: Verify red**

Run: `go test ./middleware -run 'TestResponseCaptureMiddleware' -count=1`

Expected: the `both disabled` case fails because capture is currently unconditional.

- [ ] **Step 3: Add the pass-through guard**

At the start of the returned handler in `ResponseCaptureMiddleware`:

```go
if !common.CosLogEnabled && !common.RequestLogEnabled {
	c.Next()
	return
}
```

- [ ] **Step 4: Verify green and commit**

Run: `go test ./middleware -run 'TestResponseCaptureMiddleware' -count=1`

Expected: PASS for all four flag combinations and the existing capture assertions.

```bash
git add middleware/response_capture.go middleware/response_capture_test.go
git commit -m "fix: bypass response capture when logging is off"
```

---

### Task 4: Guard MySQL request-log writes without affecting reads

**Files:**
- Modify: `model/request_log.go`
- Create: `model/request_log_test.go`

**Interfaces:**
- Consumes: `common.RequestLogEnabled`.
- Produces: no `LOG_DB.Create` or response `Updates` call when disabled.
- Preserves: `GetRequestLogById` and schema migration behavior.

- [ ] **Step 1: Write failing create/update guard tests**

```go
package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func withRequestLogDisabled(t *testing.T) {
	oldEnabled := common.RequestLogEnabled
	oldDB := LOG_DB
	common.RequestLogEnabled = false
	LOG_DB = nil
	t.Cleanup(func() {
		common.RequestLogEnabled = oldEnabled
		LOG_DB = oldDB
	})
}

func TestRecordRequestLogDisabledDoesNotTouchDatabase(t *testing.T) {
	withRequestLogDisabled(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	recordRequestLog(c, 1, 1, "user", "model", 1, "request-id")
}

func TestFlushRequestLogResponsesDisabledDoesNotTouchDatabase(t *testing.T) {
	withRequestLogDisabled(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ctxKeyRequestLogIds, []int{1})
	FlushRequestLogResponses(c, `{"X-Test":"value"}`, `{"ok":true}`)
}
```

- [ ] **Step 2: Verify red**

Run: `go test ./model -run 'Test(Record|Flush)RequestLog' -count=1`

Expected: tests panic/fail because the current code dereferences `LOG_DB`.

- [ ] **Step 3: Implement minimal guards**

Change the first condition of both write functions:

```go
if !common.RequestLogEnabled || c == nil || logId <= 0 {
	return
}
```

and:

```go
if !common.RequestLogEnabled || c == nil {
	return
}
```

- [ ] **Step 4: Verify green and commit**

Run: `go test ./model -run 'Test(Record|Flush)RequestLog' -count=1`

Expected: PASS without a database connection.

```bash
git add model/request_log.go model/request_log_test.go
git commit -m "feat: allow request log writes to be disabled"
```

---

### Task 5: Document flags and run complete verification

**Files:**
- Modify: `.env.example`
- Verify: all modified Go packages and production Docker build.

**Interfaces:**
- Documents: `COSLOG_ENABLED=false` and `REQUEST_LOG_ENABLED=true`.
- Produces: uniquely tagged image `new-api:<commit>-logging-switches`.

- [ ] **Step 1: Add example configuration**

```dotenv
# Full request/response logging
COSLOG_ENABLED=false
REQUEST_LOG_ENABLED=true
```

- [ ] **Step 2: Run formatting and diff checks**

```bash
gofmt -w common/logging_flags_test.go common/constants.go common/init.go \
  pkg/coslog/config.go pkg/coslog/log_entry.go pkg/coslog/log_entry_test.go \
  middleware/response_capture.go middleware/response_capture_test.go \
  model/request_log.go model/request_log_test.go
git diff --check
```

Expected: no formatting or whitespace errors.

- [ ] **Step 3: Run focused and repository package tests**

```bash
go test ./common ./middleware ./model ./pkg/coslog -count=1
go list ./... | grep -v '^github.com/QuantumNous/new-api$' | xargs go test -count=1
```

Expected: all packages pass. The root package is covered by the Docker build because it requires generated embedded frontend assets.

- [ ] **Step 4: Build the production image without replacing the old tag**

```bash
tag="$(git rev-parse --short=8 HEAD)-logging-switches"
docker build --cpuset-cpus=0-3 -t "new-api:$tag" .
docker image inspect "new-api:$tag"
```

Expected: build exit code 0; `new-api:b4892b87` remains present.

- [ ] **Step 5: Commit documentation and record final source state**

```bash
git add .env.example
git commit -m "docs: document request logging switches"
git status --short
git log --oneline -6
```

Expected: clean worktree and a unique final commit for the image.

---

### Task 6: Green deployment with COSLOG off and request_logs on

**Files:**
- Modify after verification: `/root/tunnel-deploy/newapi/docker-compose.yml`
- Modify during traffic switch: `/root/nginx/conf.d/model.unbound-future.ai.conf`
- Modify during traffic switch: `/root/nginx/conf.d/model-2.unbound-future.ai.conf`
- Create temporarily: root-only green environment file; delete after green container creation.

**Interfaces:**
- Consumes: verified new image tag.
- Produces: `new-api-green` on `newapi-network` with current environment and volume.
- First rollout: `COSLOG_ENABLED=false`, `REQUEST_LOG_ENABLED=true`.

- [ ] **Step 1: Create fresh deployment backups**

```bash
deploy_state="/root/tunnel-deploy/newapi/codex-logging-switches-$(date -u +%Y%m%dT%H%M%SZ)"
umask 077
mkdir -p "$deploy_state"
chmod 700 "$deploy_state"
cp -p /root/tunnel-deploy/newapi/docker-compose.yml "$deploy_state/docker-compose.yml.before"
cp -p /root/nginx/conf.d/model.unbound-future.ai.conf "$deploy_state/model.unbound-future.ai.conf.before"
cp -p /root/nginx/conf.d/model-2.unbound-future.ai.conf "$deploy_state/model-2.unbound-future.ai.conf.before"
docker image inspect new-api:b4892b87 --format '{{.Id}}' > "$deploy_state/old-image-id.txt"
sha256sum "$deploy_state"/*.before > "$deploy_state/before.sha256"
```

Expected: a mode-700 directory containing only deployment configuration and identifiers. Do not copy `/data`, `request_logs.ibd`, or request payload files.

- [ ] **Step 2: Create green from the current container configuration without printing secrets**

Generate a mode-600 environment file directly from `docker inspect new-api`, replace only the two logging flags, and never print the file:

```bash
env_file="$deploy_state/new-api-green.env"
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' new-api |
awk '
  BEGIN { coslog = 0; requestlog = 0 }
  /^COSLOG_ENABLED=/ { print "COSLOG_ENABLED=false"; coslog = 1; next }
  /^REQUEST_LOG_ENABLED=/ { print "REQUEST_LOG_ENABLED=true"; requestlog = 1; next }
  { print }
  END {
    if (!coslog) print "COSLOG_ENABLED=false"
    if (!requestlog) print "REQUEST_LOG_ENABLED=true"
  }
' > "$env_file"
chmod 600 "$env_file"
```

Start the green container:

```bash
docker run -d --name new-api-green \
  --network newapi-network \
  --volumes-from new-api \
  --restart=no \
  --env-file "$env_file" \
  "new-api:$tag"
rm -f "$env_file"
```

Delete the temporary environment file immediately after container creation.

- [ ] **Step 3: Verify green before traffic**

Check container health/status, `/api/status`, login error handling, log list/detail, one controlled model request, quota/batch markers, runtime flag values, absence of COSLOG initialization/new local files, and continued `request_logs` high-water growth. Do not print credentials or payloads.

- [ ] **Step 4: Switch Nginx gracefully**

Change only `http://new-api:3000` to `http://new-api-green:3000` in both virtual hosts. Run `nginx -t` inside the Nginx container, then `nginx -s reload`. Do not restart Nginx or MySQL.

- [ ] **Step 5: Observe and enforce rollback thresholds**

Observe at least 10 minutes:

- HTTP 5xx and database errors do not increase.
- Login, log page, model requests, quota settlement, and batch flush continue.
- `request_logs` continues growing because it remains enabled.
- COSLOG local files stop growing.
- `new-api-green` RSS is stable or materially below blue.

Rollback immediately if health checks fail, error rate rises, or functional checks fail: restore the two Nginx files, run `nginx -t`, reload, and leave the green container stopped for inspection.

- [ ] **Step 6: Reconcile the managed Compose service only after green is stable**

Update the Compose image to the verified tag, set `COSLOG_ENABLED=false`, and add `REQUEST_LOG_ENABLED=true`. While Nginx still points to green, drain and replace the Compose-managed `new-api`, verify it directly, switch Nginx back to `http://new-api:3000`, reload, then remove `new-api-green`. Keep `new-api:b4892b87` and all timestamped backups.

- [ ] **Step 7: Final verification**

Record container image IDs, running status, runtime flag values, Nginx target, HTTP checks, MySQL connections/transactions/locks, application RSS, COSLOG file high-water mark, `request_logs` high-water mark, Git commit, image tag, backup path, and every changed file. Confirm no temporary environment file remains.
