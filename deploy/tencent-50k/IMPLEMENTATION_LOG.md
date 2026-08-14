# 实施记录

更新时间：2026-08-14（Asia/Shanghai）

## 单机 ClickHouse 调整

- 腾讯部署模板已从托管分布式 ClickHouse 改为单机模式。
- 应用配置使用 `LOG_SQL_MANAGED_SCHEMA=false`，删除 `LOG_SQL_CLICKHOUSE_CLUSTER` 和 `LOG_SQL_CLICKHOUSE_LOCAL_TABLE`。
- 建表脚本只创建 `newapi_logs.logs`，表引擎为 `MergeTree`；不再创建 `logs_local`、`Distributed` 表或执行 `ON CLUSTER` DDL。
- 验证脚本直接读取本机 `system.parts`，不再调用 `clusterAllReplicas`。
- `LOG_SQL_CLICKHOUSE_TTL_DAYS=0`，日志永久保留，不设置自动清理。
- 通用代码中的分布式兼容能力仍保留但不启用，避免影响其他部署方式和未来扩展。
- 本次只修改代码与部署模板，没有创建、启动或恢复任何腾讯云资源。

## 关闭记录

因当前方案固定成本过高，已于 2026-08-12 关闭整套 `newapi-prod` 计费资源：

- ASG 先设置为 `MinSize=0 / DesiredCapacity=0 / MaxSize=0`，确认 10 台实例和 20 块随实例云盘全部释放后，删除三条扩容策略、ASG 和启动配置。
- 删除 CLB、HTTP 监听器和 900 秒个性化超时配置。
- PostgreSQL 关闭删除保护后隔离；按量实例已停止计费，暂处官方回收窗口。
- Redis 执行按量实例销毁；当前为“待删除”，已停止计费。
- ClickHouse 执行集群销毁，已经从活动实例列表移除。
- 精确复核结果：`newapi-prod` 相关 CVM、CBS、EIP、CLB、ASG 和启动配置均为 0。
- 保留零固定费用的 VPC、两个子网、两个安全组、TCR 个人版仓库与镜像，供下一版低成本方案复用。
- COS 旧桶及历史数据未删除；新前缀 `us/tencent-prod/` 关闭时仍为 0 个对象。
- 独立测试机 `test-new`、w-new 和 GCP 环境不属于本次关闭范围，没有操作。

## 已完成

- 从 GitHub `2026-08-07-main` 的 `7d491bfc` 建立独立分支 `deploy/tencent-50k`。
- 取消单独控制节点；账单 Worker 使用 PostgreSQL 租约在同构应用节点间选主和自动接替。
- 账单聚合表、状态表和任务表固定写 PostgreSQL；普通日志继续写 ClickHouse。
- ClickHouse 账单扫描游标改为 `(created_at, request_id)`，并预留 120 秒异步写入安全窗口。
- 增加托管分布式 ClickHouse 表校验和分区化清理逻辑。
- COSLOG 增加异步上传、失败重试、启动扫描、停机 flush、主机名文件前缀和 85% 磁盘保护。
- 增加腾讯云应用 Compose、配置模板、ClickHouse DDL、实例启动脚本和验证脚本。
- 推送 GitHub 分支 `deploy/tencent-50k`。
- 使用目标账号 `100041294359` 在 `na-ashburn` 创建独立的 VPC、跨可用区子网、安全组、TCR、PostgreSQL、Redis、TCHouse-C、CLB、启动配置和 ASG。
- 构建并推送固定 digest 镜像：`useccr.ccs.tencentyun.com/newapi-prod/new-api@sha256:35876fce5a1ca7323d9e5c43206cc368bcb454072ed0f401f40a058f4d55aeb3`。
- 从空库初始化 NewAPI，创建独立 Root；没有迁移或读取其他 NewAPI 环境的业务数据。
- 先验证 1 台，再验证跨区 2 台，最后扩到 10 台；生产最低实例数为 10，最高为 16。
- CLB 使用最小连接数调度、`/api/status` 健康检查和 900 秒七层读取/发送超时。
- 配置 CPU、内存和 TCP 已建立连接数三条扩容规则；不配置自动缩容，避免主动中断流式连接。
- 删除未使用且磁盘策略不兼容的旧启动配置 `asc-oicex0jk`；保留有效启动配置 `asc-3u06oali`。
- 清除测试机 Docker 客户端中用于构建镜像的 TCR 登录凭据；测试机运行中的 NewAPI 未加入生产 CLB/ASG。

## 验证结果

- Go 测试通过：`common`、`model`、`service`、`pkg/coslog`。
- 完整 Docker 镜像构建成功。
- 临时 ClickHouse 验证：分布式 `logs` 写入、读取、结构校验和历史清理均成功。
- 临时 SQLite + ClickHouse 验证：空环境初始化、健康检查、账单读取均成功。
- 临时 PostgreSQL + ClickHouse 双应用实例验证：同一日志最终只有 1 行账单、`call_count=1`，无重复聚合。
- 验证过程中发现并修正 PostgreSQL UPSERT 列名歧义；修正后重新验证通过。
- 临时容器与网络均已删除；原 `test-new` 容器保持运行且健康。
- PostgreSQL 空环境初始化验证：`users=1`、`setups=1`、`billing_report_state=1`、`billing_report_jobs=0`。
- Redis AUTH/PING 验证通过；ClickHouse 分布式表可写可查，生产应用日志已进入 `newapi_logs.logs`。
- ASG 当前 10 台均为 `IN_SERVICE/HEALTHY`，两个可用区各 5 台；CLB 后端 10/10 为 `Alive`。
- 10 台容器均运行同一固定 digest，`/api/status` 正常，配置文件权限为 `0600`，1 TB XFS 数据盘已挂载。
- 通过 CLB 连续执行 20 次 Root 鉴权请求，20/20 返回成功。
- COS 写入、HEAD 和删除测试通过；测试对象已删除。

## 已创建资源

| 类型 | 名称 / ID | 说明 |
|---|---|---|
| VPC | `newapi-prod-vpc` / `vpc-fz1s8ndx` | `10.60.0.0/16` |
| 子网 | `subnet-lkyodqyc`、`subnet-7zdq4u2g` | `na-ashburn-1/2`，各 1 个 |
| 安全组 | `sg-46osxvb3`、`sg-9g9hx1rr` | 应用与数据服务分离 |
| TCR | `newapi-prod/new-api` | 私有镜像仓库 |
| PostgreSQL | `newapi-prod-pg` / `postgres-hatubhdi` | PostgreSQL 16，8C64G，1 TB，私网 `10.60.1.7:5432` |
| Redis | `newapi-prod-redis` / `crs-lctlf7vc` | Redis 7 Standard，16 GB，私网 `10.60.1.3:6379` |
| ClickHouse | `newapi-prod-clickhouse` / `cdwch-1xf2obdz` | 4 分片、无数据副本，私网 `10.60.1.4:9000/8123` |
| CLB | `newapi-prod-clb` / `lb-bnly7h9z` | 当前 HTTP 80 验证入口，10 个健康后端 |
| CLB 超时配置 | `newapi-prod-l7-timeouts` / `pz-fl6e19n9` | 读、发、客户端发送均 900 秒 |
| 启动配置 | `newapi-prod-launch-v3` / `asc-3u06oali` | 8C32G、100 GB 系统盘、1 TB 数据盘、固定镜像 digest |
| ASG | `newapi-prod-asg` / `asg-0lxkg8wu` | 最低/期望 10，最高 16，跨区均衡 |
| COS | `newlog-data-1346826778/us/tencent-prod/` | 完整 COSLOG 的生产前缀 |

## 保持不变与待办

- COSLOG 保持现有行为：1 万条或 120 秒只刷新本地缓冲；约 100 MB 或进程退出才轮转并触发上传。没有修改该逻辑。
- 当前只有 CLB HTTP 80 验证入口，尚未创建 HTTPS 443 监听器，也没有修改生产 DNS。正式启用前需要确定域名并提供/申请证书。
- 新环境是空业务库；正式引流前仍需配置用户、令牌、渠道、价格和其他系统设置。
- 未进行 5 万 RPM 压测；本次只做基础设施、健康检查、共享鉴权和数据链路验证。
- 最早的 2 台实例来自启动配置 v2，因 ClickHouse 初始化参数问题已人工修复并验证健康；后续 8 台及未来扩容均使用已修正的 v3。
- 没有自动缩容策略。需要缩容时先确认长连接已排空，再人工逐步降低实例数。
