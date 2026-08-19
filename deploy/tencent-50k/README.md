# 腾讯云上海 NewAPI 手动扩容部署

本目录部署一套空业务库的上海环境，不读取或修改 w-new、GCP、test-new。固定数据节点 `new-1` 永久运行 PostgreSQL、Redis、单机 ClickHouse、NewAPI 和 Nginx；后续应用节点只运行 NewAPI 和 Nginx，通过私网共享数据服务。

## 固定资源

| 资源 | 配置 |
|---|---|
| 数据节点 | `new-1` / `ins-eu2z6brd`，私网 `10.0.0.14` |
| VPC / 子网 | `vpc-l5386ebs` / `subnet-geb2r0m7` |
| SSH 密钥 | `skey-824uathx`（`new.pem`） |
| 应用节点 | `SA5.2XLARGE32`，竞价，Ubuntu 22.04，200 GB SSD，200 Mbps 按流量公网 |
| TCR | `ccr.ccs.tencentyun.com/unbound-newapi/new-api`（`newapi-tencent` 为 TCR 保留名称，不能作为命名空间） |
| COSLOG | `newlog-1346826778/data/`，`ap-shanghai`，100% 采样 |
| ClickHouse | 单机 `newapi_logs.logs`，无分布式表、无 TTL |

## 文件

- `compose-data.yaml`：仅用于 `new-1`，包含全部数据服务和应用。
- `compose-app.yaml`：用于手工新增的无状态应用节点。
- `newapi.env.example`：两类节点共用的配置模板，不包含真实密钥。
- `nginx.conf`：关闭流式缓冲并将代理超时固定为 900 秒。
- `scripts/bootstrap-data.sh`：初始化 `/dev/vdb` 为 XFS 并启动数据节点。
- `scripts/bootstrap-app.sh`：在应用节点启动 NewAPI 和 Nginx。

部署文件放置在 `/opt/newapi`，真实配置放置在 `/etc/newapi/newapi.env` 并设为 `0600`。镜像必须使用上海 TCR 的固定 digest。

## 数据节点启动

```bash
sudo /opt/newapi/scripts/bootstrap-data.sh
sudo /opt/newapi/scripts/verify-data.sh
```

脚本只会在 `/dev/vdb` 没有文件系统时格式化；如果发现已有非 XFS 文件系统会停止，避免覆盖数据。PostgreSQL、Redis、ClickHouse 和应用数据均保存到 `/data`。

## 应用节点启动

```bash
sudo /opt/newapi/scripts/bootstrap-app.sh
sudo /opt/newapi/scripts/verify-app.sh
```

应用节点不保存业务数据库，只保留 COSLOG 上传前的本地缓冲和进程日志。竞价实例被回收时，尚未上传的本地缓冲可能丢失。

## 手动扩缩容

扩容时按相同规格创建新 CVM，复制同一固定 digest、部署文件和 `newapi.env`，验证 `/api/status` 后绑定到 CLB 的 80 端口。缩容时先从 CLB 摘除，等待最长流式请求结束，再关闭实例。

CLB 使用 `LEAST_CONN`、关闭会话保持、健康检查 `GET /api/status`，读取和发送超时均为 900 秒。固定 `new-1` 始终保留为后端和应急直连入口。

## 明确保持不变

- COSLOG 达到 1 万条或 120 秒时刷新本地缓冲，约 100 MB 或进程退出时轮转并上传。
- `LOG_SQL_CLICKHOUSE_TTL_DAYS=0`，不自动删除普通日志。
- `REQUEST_LOG_ENABLED=false`，普通 logs、账单和额度统计正常记录。
- 不创建 ASG、启动配置或自动伸缩策略。
