# 用户调用记录 COS 归档 — 设计文档

- 日期：2026-05-13
- 范围：将每条 relay 请求/响应完整记录异步写入本地 jsonl 文件，满 100MB 自动上传腾讯云 COS
- 状态：待实现

## 1. 目标

为运维和审计提供完整的用户调用记录，字段包含 user_name、model_name、url、request_id、request_body、response_body 等。每条记录写入 jsonl，本地缓存，单文件达到 100MB 后自动上传腾讯云 COS 并删除本地文件。

## 2. 架构

沿用现有结算钩子模式（与 `pkg/prom_metrics` 同层接入），复用现有 `BodyStorage` 读取 request body。

```
HTTP 请求
  ↓
BodyStorage (request body, 已有)
ResponseCaptureMiddleware (包装 gin.ResponseWriter, 缓存 response body/headers)
  ↓
relay 处理 → 写 response (被 ResponseCaptureWriter 实时缓存)
  ↓
结算钩子 service/quota.go / service/text_quota.go
  - 从 BodyStorage 读 request body
  - 从 ResponseCaptureWriter 读 response body/headers
  - 组装 COSLOG → coslog.Record()
  ↓
pkg/coslog/ JSONLWriter 后台 goroutine
  - 批量 flush → 本地 jsonl
  - 文件 >100MB → 上传 COS → 删本地 → 新建文件
  - 定时 flush (120s)
  - Close 时 flush 并上传最后一个文件
```

## 3. 包结构

```
pkg/coslog/
  log_entry.go   — COSLOG 结构体定义
  writer.go      — JSONLWriter: 异步批量写本地 jsonl, 自动轮转, 上传 COS
  cos.go         — 腾讯云 COS 客户端封装 (PutFromFile)
  config.go      — 环境变量解析与默认值
  writer_test.go — 单元测试

middleware/response_capture.go — ResponseCaptureWriter + ResponseCaptureMiddleware
```

## 4. 字段定义

```go
type COSLOG struct {
    UserName        string `json:"user_name"`
    ModelName       string `json:"model_name"`
    Url             string `json:"url"`
    RequestID       string `json:"request_id"`
    RequestBody     string `json:"request_body"`
    RequestHeaders  string `json:"request_headers"`
    ResponseHeaders string `json:"response_headers"`
    ResponseBody    string `json:"response_body"`
}
```

- `user_name` — `c.GetString("username")`
- `model_name` — `relayInfo.OriginModelName`
- `url` — `c.Request.URL.String()`
- `request_id` — `common.GetContextKeyString(c, common.RequestIdKey)`
- `request_body` — 从 `common.GetRequestBody(c)` 读取后转 string
- `request_headers` — `common.FormatMap(common.GetRequestHeaders(c))`
- `response_headers` — ResponseCaptureWriter 缓存的 headers
- `response_body` — ResponseCaptureWriter 缓存的 body

## 5. JSONLWriter 行为

### 5.1 初始化

在 `main.go` 的 `InitResources()` 末尾调用 `coslog.Init()`，由环境变量决定是否启动：

```go
func Init() {
    cfg := loadConfig()
    if !cfg.Enabled {
        return
    }
    writer, err := NewJSONLWriter(cfg)
    if err != nil {
        common.SysError("coslog init failed: " + err.Error())
        return
    }
    defaultWriter = writer
}
```

不采用 `init()` 硬编码模式，失败时优雅降级（只打日志，不影响主服务）。

### 5.2 写入流程

```
coslog.Record(entry) → 非阻塞写入 channel (buffer 10000)
  ↓
后台 goroutine:
  - 批量满 flushSize (默认 10000) → flushBuffer
  - 或定时器 120s → flushBuffer
  - 或进程退出 Close() → flushBuffer + 上传最后一个文件

flushBuffer:
  1. 将 buffer 中每条记录 Marshal 为 JSON，追加 '\n'，写入当前本地文件
  2. 检查文件大小
     - fileSize <= maxFileSize: 继续
     - fileSize > maxFileSize:
       a. 关闭当前文件
       b. uploadToCOS(oldFile)
       c. delete local file (若配置)
       d. newFile()
  3. 清空 buffer
```

### 5.3 文件命名

```
{prefix}_{timestamp}_{random6}.jsonl
例: log_20260513_143052_123456.jsonl
```

### 5.4 COS 上传

- objectKey: `{cosPrefix}/{filename}` (prefix 为空时直接用 filename)
- 使用 `github.com/tencentyun/cos-go-sdk-v5` 的 `Object.PutFromFile`
- 上传失败只打 `common.SysError`，不阻塞、不重试，避免积压
- 上传后根据配置删除本地文件

## 6. 中间件

### 6.1 ResponseCaptureMiddleware

只挂在 relay 路由上，紧跟 `prom_metrics.GinMiddleware()`：

```go
func ResponseCaptureMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        cw := &captureWriter{
            ResponseWriter: c.Writer,
            body:           &bytes.Buffer{},
            headers:        make(http.Header),
        }
        c.Writer = cw
        c.Next()
        // 请求结束后将捕获的 headers/body 写入 context，供结算钩子读取
        c.Set(ctxKeyResponseBody, cw.body.String())
        c.Set(ctxKeyResponseHeaders, common.FormatMap(headersToMap(cw.headers)))
    }
}
```

### 6.2 与 BodyStorageCleanup 的协作

中间件挂载顺序（在 `router/relay-router.go`）：

```go
router.Use(middleware.CORS())
router.Use(middleware.DecompressRequestMiddleware())
router.Use(middleware.BodyStorageCleanup())   // 进入时无操作, 退出时清理 BodyStorage
router.Use(middleware.StatsMiddleware())
router.Use(prom_metrics.GinMiddleware())
router.Use(middleware.ResponseCaptureMiddleware()) // 新增
```

结算钩子在 handler 内部执行（`c.Next()` 返回前），此时 BodyStorage 和 ResponseCapture 缓存均存活。

## 7. 结算钩子接入

在 `service/quota.go` 和 `service/text_quota.go` 的 `PostConsumeQuota` / `PostTextConsumeQuota` 末尾，与 `prom_metrics.RecordRelaySettled` 并列：

```go
gopool.Go(func() {
    prom_metrics.RecordRelaySettled(relayInfo, prom_metrics.SettledSample{...})
    coslog.Record(ctx, relayInfo)
})
```

`coslog.Record` 内部：
1. 从 `ctx` 读取 ResponseCapture 写入的 response body/headers
2. 从 `common.GetRequestBody(ctx)` 读取 request body
3. 组装 `COSLOG`
4. 调用 `defaultWriter.Write(entry)`
5. 顶层 `defer recover`，任何异常只打 `common.SysError`，不影响主业务

## 8. 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `COSLOG_ENABLED` | `false` | 总开关。`false` 时跳过初始化，零开销 |
| `COS_BUCKET` | — | 腾讯云 COS bucket |
| `COS_REGION` | — | 腾讯云 COS region，如 `ap-beijing` |
| `COS_PREFIX` | `newapi-logs` | COS 对象前缀 |
| `COS_SECRET_ID` | — | SecretId |
| `COS_SECRET_KEY` | — | SecretKey |
| `COSLOG_FLUSH_SIZE` | `10000` | 批量 flush 条数 |
| `COSLOG_FLUSH_INTERVAL` | `120` | 定时 flush 间隔（秒） |
| `COSLOG_MAX_FILE_SIZE` | `104857600` | 单文件大小限制（字节），默认 100MB |
| `COSLOG_LOCAL_DIR` | `./oss_log` | 本地临时目录 |
| `COSLOG_DELETE_AFTER_UPLOAD` | `true` | 上传后是否删除本地文件 |

## 9. 错误处理与基数控制

### 9.1 主路径零影响

- `coslog.Record` 顶层 `defer recover`
- channel 满时阻塞等待（buffer 10000，正常不会满）
- 如果 `defaultWriter == nil`（初始化失败或未开启），直接 return
- request/response body 读取失败时，对应字段填 `""`，不打错误日志（避免日志风暴）

### 9.2 流式响应大小控制

流式请求的 response body 可能很大（SSE 逐条累积）。`ResponseCaptureWriter` 的 `body` 使用 `bytes.Buffer`，不设硬上限。若后续出现 OOM，可追加 `maxCaptureBytes` 配置进行截断。

### 9.3 并发安全

- `JSONLWriter` 内部使用 `sync.Mutex` 保护 buffer 和 file 操作
- `Write(data)` 将数据推入 channel（channel 本身是并发安全的）
- 后台 goroutine 单线程处理 flush

## 10. 测试策略

| 测试文件 | 覆盖点 |
|---|---|
| `pkg/coslog/writer_test.go` | 初始化 `Enabled=false` 时不启动；正常写入后文件存在且为合法 jsonl；文件大小超过限制后自动轮转；Close 时 flush 并生成新文件 |
| `middleware/response_capture_test.go` | 包装 `gin.ResponseWriter` 后 `Write` 的数据同时写入原始 writer 和缓存；`c.Next()` 后能从 context 读出 response body |

## 11. 依赖

- `github.com/tencentyun/cos-go-sdk-v5`（新增 direct require）
- 复用现有 `github.com/bytedance/gopkg/util/gopool`
