# Nacos 配置引导

核心服务直接启动，无 flag、`-config`、`-check-ready` 或本地配置回退。各 main 保留自己的固定服务身份和独立 runService；配置中心只提供数据，不负责按名称分发启动服务。

## 环境变量

| 变量 | 含义 |
|---|---|
| NACOS_ADDRESS | 必填 TCP host:port，不带 URL 或凭据 |
| NACOS_NAMESPACE | 必填明确的命名空间 ID，不接受 public |
| NACOS_GROUP | 必填配置及服务发现分组 |
| NACOS_CLUSTER | 必填注册发现 cluster |
| NACOS_USERNAME / NACOS_PASSWORD | 必填 Nacos 认证信息，只在 env 中 |
| NACOS_DATA_ID | 核心默认为 market.json / strategy.json / execution.json / portfolio.json；可为多实例指定不同 Data ID；通用维护/探针工具必须提供 |
| NACOS_TLS_ENABLED | 默认 true，本地现有 infra 显式 false |
| NACOS_ROOT_CERT_FILE | 可选 Nacos HTTPS 根证书文件 |
| NACOS_CONNECT_TIMEOUT / NACOS_COMMAND_TIMEOUT | 默认 5s / 2s，可显式设置 1ms–30s |

可公开示例见 [nacos.env.example](../configs/examples/nacos.env.example)，均为示例值。开发者在 shell 导出这些变量后直接运行 `.cache/bin/strategy`；Docker Compose 从忽略的 `.cache/compose/nacos.env` 注入连接信息，为每个容器设置对应 NACOS_DATA_ID。

## 配置内容

内容使用 `deer/proto/config/v1/service.proto` 对应的严格 Proto JSON，未知字段、空配置、错误身份或非法字段均使启动失败。包括监听、日志、PostgreSQL/Redis/JetStream 连接参数、RPC 身份与限制、业务账户/行情绑定、租约/轮询等运行配置。金额/价格/数量继续使用十进制字符串。数据库/消息密码仍用已约定的 password_env 引用注入，不发布真实密码。

Nacos 连接信息不能写在内容里；`nacos` 字段只允许保留服务自身的 `registration`（advertise_ip/heartbeat_interval）。连接和认证字段始终来自 env，远程内容试图覆盖时直接拒绝。证书/密钥路径为服务挂载的文件路径，连接配置不携带私钥。

本地持久配置位于 namespace `muskdeer-local`、group `MUSKDEER_CORE` 的四个 Data ID。`scripts/compose.py prepare` 显式首次发布缺失项，已有内容保持，不会在每次启动时覆盖控制台编辑。市场绑定首次初始化后再发布。草稿在 `.cache/compose/nacos-drafts` 供查看，容器仅挂载自身证书目录，不挂载服务 JSON。

读取使用已部署 Nacos 2.4 的 [V2 配置 Open API](https://nacos.io/en/docs/v2/open-api/)：`GET /nacos/v2/cs/config`，认证放在请求头，范围由 namespaceId/group/dataId 明确指定。读取有期限和大小上限，错误不回显配置、口令或 token。Bootstrap transport 读取后关闭，服务再由自己的 Deer 管理注册发现及其他依赖。

配置在启动时读取一次并成为当前进程的快照；编辑后显式重启相应服务，不通过配置热更新绕过持久参数版本、风控或最终 fencing。订单、余额、持仓及策略审计/版本等持久业务状态继续由 PostgreSQL 管理，不转移为配置项。

## 探针与维护

`/readycheck` 为独立、无参数程序，读取同一个 Data ID 后只探测已运行服务的 loopback readyz，不构造第二个 Deer、数据库池或业务工作器。探针超时 6s；Compose 给它 8s。配置中心无法访问或配置无效时探针失败。

```sh
make compose-up
docker compose --env-file .cache/compose/compose.env ps
docker compose --env-file .cache/compose/compose.env exec -T strategy /readycheck
```

显式迁移和 coreinit 默认同样读取 Nacos。`migrate -apply` 仍保留防误执行开关；历史测试使用的维护工具 `-config` 仅作兼容，不存在于核心服务入口。tools profile 的一次性维护容器只连接 infra 网络，避免与正在运行的核心容器争用固定 IP。

`make core-test` 把每个真实测试进程的独立配置发布到临时 Nacos 项，再无参数启动实际核心二进制；进程停止后删除配置，测试命名空间与持久运行配置隔离。镜像测试也连接真实 infra Nacos，验证没有本地 JSON 挂载/参数、非 root/只读运行及退出；旧禁网启动合同因必须读取 Nacos 而由明确的 infra 网络访问替代，不部署额外基础服务。
