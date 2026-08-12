# 实施记录

更新时间：2026-08-12（Asia/Shanghai）

## 已完成

- 从 GitHub `2026-08-07-main` 的 `7d491bfc` 建立独立分支 `deploy/tencent-50k`。
- 取消单独控制节点；账单 Worker 使用 PostgreSQL 租约在同构应用节点间选主和自动接替。
- 账单聚合表、状态表和任务表固定写 PostgreSQL；普通日志继续写 ClickHouse。
- ClickHouse 账单扫描游标改为 `(created_at, request_id)`，并预留 120 秒异步写入安全窗口。
- 增加托管分布式 ClickHouse 表校验和分区化清理逻辑。
- COSLOG 增加异步上传、失败重试、启动扫描、停机 flush、主机名文件前缀和 85% 磁盘保护。
- 增加腾讯云应用 Compose、配置模板、ClickHouse DDL、实例启动脚本和验证脚本。
- 推送 GitHub 分支 `deploy/tencent-50k`。

## 验证结果

- Go 测试通过：`common`、`model`、`service`、`pkg/coslog`。
- 完整 Docker 镜像构建成功。
- 临时 ClickHouse 验证：分布式 `logs` 写入、读取、结构校验和历史清理均成功。
- 临时 SQLite + ClickHouse 验证：空环境初始化、健康检查、账单读取均成功。
- 临时 PostgreSQL + ClickHouse 双应用实例验证：同一日志最终只有 1 行账单、`call_count=1`，无重复聚合。
- 验证过程中发现并修正 PostgreSQL UPSERT 列名歧义；修正后重新验证通过。
- 临时容器与网络均已删除；原 `test-new` 容器保持运行且健康。

## 未创建的云资源

尚未创建 VPC、子网、安全组、TCR、PostgreSQL、Redis、TCHouse-C、CLB、启动配置或 ASG，也没有产生本方案对应的云资源购买。原因是本机腾讯云 API 凭据属于账号 `100042235376`，而目标实例和控制台属于账号 `100041294359`；现有目标账号 CSV 密钥验证失败。待取得目标账号可用 API 凭据并完成购买确认后继续。

