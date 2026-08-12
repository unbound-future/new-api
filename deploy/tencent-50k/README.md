# 腾讯云 NewAPI 5 万 RPM 部署（无控制节点）

这套目录用于创建一套全新的腾讯云环境，不迁移、读取或修改现有业务环境。所有应用节点运行完全相同的镜像与配置，不再创建单独控制节点。账单 Worker 会在所有实例启动，但通过 PostgreSQL 租约保证同一时间只有一台执行；实例退出后其他节点可自动接手。

## 资源清单与命名

以下名称是部署时的固定命名约定：

| 资源 | 名称 | 用途 |
|---|---|---|
| VPC | `newapi-prod-vpc` | 隔离生产网络 |
| 子网 A | `newapi-prod-subnet-a` | `na-ashburn-1` 应用与托管服务私网 |
| 子网 B | `newapi-prod-subnet-b` | `na-ashburn-2` 应用节点私网 |
| 应用安全组 | `newapi-prod-app-sg` | 仅允许 CLB 到 3000、管理来源到 22 |
| 数据安全组 | `newapi-prod-data-sg` | 仅允许应用安全组访问 PostgreSQL、Redis、ClickHouse |
| TCR 命名空间/仓库 | `newapi-prod/new-api` | 保存固定 digest 的生产镜像 |
| PostgreSQL | `newapi-prod-pg` | 用户、令牌、渠道、额度、系统配置和账单聚合 |
| Redis | `newapi-prod-redis` | 多实例共享缓存、会话和分布式状态 |
| TCHouse-C | `newapi-prod-clickhouse` | 普通 `logs` 分布式存储 |
| CLB | `newapi-prod-clb` | 当前 HTTP 验证入口；正式域名确定后增加 HTTPS |
| 启动配置 | `newapi-prod-launch-v3` | 固定机型、镜像 digest、数据盘和启动脚本 |
| 弹性伸缩组 | `newapi-prod-asg` | 管理 10～16 台同构 NewAPI 实例 |
| COS 前缀 | `newlog-data-1346826778/us/tencent-prod/` | 保存抽样后的完整 COSLOG |

现有 `test-new` 只做功能验证，不加入 CLB 或伸缩组。

## 精简后的拓扑

```text
API 域名（DNS 直连，不经过 Cloudflare 代理）
  -> 公网 CLB（当前验证使用 :80；正式启用后使用 :443）
  -> newapi-prod-asg（初始/最少 10，最多 16）
     -> PostgreSQL（业务与账单聚合）
     -> Redis（共享状态）
     -> ClickHouse（普通 logs）
     -> COS（完整 COSLOG）
```

## 创建顺序

1. 创建 VPC、两个子网和两个安全组。
2. 创建 TCR 仓库，构建镜像并按 digest 固定。
3. 创建 PostgreSQL 16、Redis 7 和 4 分片单副本 TCHouse-C。
4. 用 `clickhouse/schema.sql.tpl` 创建 `logs_local` 和分布式 `logs`。
5. 先启动一台应用实例，完成空数据库初始化并创建新环境 Root。
6. 创建 CLB、启动配置和 ASG，先扩到 2 台验证，再扩到 10 台。
7. 配置 CPU 60%、内存 70% 或 TCP 已建立连接数 1800 持续 3 分钟时每次增加 2 台。暂不自动缩容，避免中断流式连接。
8. 分阶段压测后再切换正式 API 域名。

## 应用文件位置

- Compose：`/opt/newapi/compose.yaml`
- 运行配置和密钥：`/etc/newapi/newapi.env`，权限 `0600`
- COSLOG 本地缓冲：`/data/newapi/coslog`
- 程序日志：`/data/newapi/logs`
- 1 TB 数据盘：`/data`

`newapi.env.example` 中没有真实密钥。实际配置不得提交到 Git。

## ClickHouse

设置 TCHouse 控制台显示的集群名后渲染 SQL：

```bash
export CLICKHOUSE_CLUSTER='实际集群名'
./scripts/render-clickhouse-schema.sh
```

使用 TCHouse 提供的账号执行生成的 `schema.rendered.sql`。应用连接分布式表 `newapi_logs.logs`，启动时设置 `LOG_SQL_MANAGED_SCHEMA=true`，只校验表结构，不执行自动建表。

普通日志永久保留。Root 清理历史日志时，完整旧月份会删除 `logs_local` 分区，只有截止日期所在月份执行一次行级删除。

## 首次启动与扩容

实例启动前，把 `compose.yaml` 放到 `/opt/newapi/`，把真实配置写入 `/etc/newapi/newapi.env`，然后运行：

```bash
sudo ./scripts/bootstrap-app.sh
sudo ./scripts/verify-app.sh
```

首次只启动 1 台完成初始化。初始化成功后，将 ASG 期望实例数设为 2 做功能验证，最后设为 10。因为所有节点共享 PostgreSQL 和 Redis，不需要复制应用数据。

## 回退

- 应用：启动配置改回上一个镜像 digest，再执行 ASG 滚动更新。
- ClickHouse：代码在 `LOG_SQL_MANAGED_SCHEMA=false` 时仍保留原单表逻辑；生产分布式表不应自动切回。
- COSLOG：上传失败会保留本地 `.jsonl` 并每 60 秒重试；磁盘达到 85% 后仅丢弃新样本，不阻塞 API。
- 数据库：PostgreSQL 开启每日备份与 PITR，保留 7 天。

## 当前生产资源状态

- 地域：`na-ashburn`
- VPC：`vpc-fz1s8ndx`
- 镜像 digest：`sha256:35876fce5a1ca7323d9e5c43206cc368bcb454072ed0f401f40a058f4d55aeb3`
- ASG：`asg-0lxkg8wu`，最低/期望 10、最高 16，两个可用区各 5 台
- CLB：`lb-bnly7h9z`，HTTP 80，最小连接数调度，健康检查 `/api/status`
- 当前验证地址：`http://lb-bnly7h9z-32gurd9tl5xpinhw.clb.use-tencentclb.com`
- PostgreSQL：`postgres-hatubhdi`
- Redis：`crs-lctlf7vc`
- ClickHouse：`cdwch-1xf2obdz`

正式开放前仍需配置业务数据、确定域名和证书，并增加 HTTPS 443 监听器；当前没有改动任何生产 DNS。

## COSLOG 上传节奏

COSLOG 沿用原有节奏，没有为本次部署改变逻辑：达到 1 万条或 120 秒时刷新到本地文件；本地文件达到约 100 MB 或进程退出时才轮转并开始上传。低流量时，COS 中看不到立即生成的新对象属于预期现象。
