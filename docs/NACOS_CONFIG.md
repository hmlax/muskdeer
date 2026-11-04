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
| NACOS_DATA_CONFIG_ID | 核心默认为 `<service>.data.yaml`；通用维护/探针必须明确提供 |
| NACOS_SERVICE_CONFIG_ID | 核心默认为 `<service>.service.yaml`；通用维护/探针必须明确提供，与 data ID 不同 |
| NACOS_TLS_ENABLED | 默认 true，本地现有 infra 显式 false |
| NACOS_ROOT_CERT_FILE | 可选 Nacos HTTPS 根证书文件 |
| NACOS_CONNECT_TIMEOUT / NACOS_COMMAND_TIMEOUT | 默认 5s / 2s，可显式设置 1ms–30s |

可公开示例见 [nacos.env.example](../configs/examples/nacos.env.example)，均为示例值。开发者在 shell 导出这些变量后直接运行 `.cache/bin/strategy`；Docker Compose 从忽略的 `.cache/compose/nacos.env` 注入连接信息，为每个容器设置两份文档 ID。旧 NACOS_DATA_ID 不再支持，显式设置会拒绝启动。

## 配置内容

每个服务发布两份严格 YAML：`data.yaml` 对应 [DataConfig](../deer/proto/common/config/v1/data.proto)，定义 PostgreSQL/Redis/JetStream 连接、密码环境变量名以及业务运行控制；`service.yaml` 对应 [ServiceSettings](../deer/proto/common/config/v1/service.proto)，定义身份、版本、启动/关闭期限、gRPC/HTTP/监控监听、TLS、日志、RPC 客户端和注册参数。示例见 [market/data.yaml](../configs/local/market/data.yaml) 与 [market/service.yaml](../configs/local/market/service.yaml)，其他服务各有独立目录。示例默认只是基础探针，不授权交易。

两份文档的 name/version 必须一致，service.yaml 的 data_sha256 必须等于 data.yaml **原始字节**的 SHA-256（包含注释和换行）。准备/发布工具自动计算并先发布 data、最后发布 service；手工编辑时可用 `shasum -a 256 data.yaml` 取得绑定值。配对中途启动会失败，已有进程保持原不可变快照。ServiceConfig 仅为内部装配结果，不是第三份配置源。

未知字段、错误分区（例如 service 中的 postgres 或 data 中的 grpc）、空配置、错误身份/版本、指纹不符、重复键、多文档、YAML anchor/alias/merge 均拒绝。所有小数必须是**带引号的字符串**，不接受浮点节点，金额/价格/数量不经过浮点转换。密码仍使用 password_env 引用，不发布真实密码。Himalaya 账户/产品/额度/网格参数继续保存在数据库，不能放回 data.yaml。

Nacos 连接和认证始终来自 env，不能写进任一文档；service.yaml 的 registration 只提供 advertise_ip/heartbeat_interval。远程覆盖 bootstrap 字段直接拒绝。证书/密钥路径指向独立挂载的文件，不携带私钥内容。

本地持久配置位于 namespace `muskdeer-local`、group `MUSKDEER_CORE` 的八个 Data ID，四服务各有 data/service 两项。`scripts/compose.py prepare` 首次显式发布，已有完整配对保持，已有半套需显式替换，不能偷偷覆盖。草稿在 `.cache/compose/nacos-drafts/<service>/{data,service}.yaml`；容器仅挂载证书目录，不挂载启动配置。Python 准备工具使用 `tools/python-requirements.txt` 固定的 PyYAML，可用 `python3 -m pip install -r tools/python-requirements.txt` 安装。

读取使用已部署 Nacos 2.4 的 [V2 配置 Open API](https://nacos.io/en/docs/v2/open-api/)：`GET /nacos/v2/cs/config`，认证放在请求头，范围由 namespaceId/group/dataId 明确指定。读取有期限和大小上限，错误不回显配置、口令或 token。Bootstrap transport 读取后关闭，服务再由自己的 Deer 管理注册发现及其他依赖。

配置在启动时读取一次并成为当前进程的快照；编辑后显式重启相应服务，不通过配置热更新绕过持久参数版本、风控或最终 fencing。订单、余额、持仓及策略审计/版本等持久业务状态继续由 PostgreSQL 管理，不转移为配置项。

## 探针与维护

`/readycheck` 为独立、无参数程序，读取同一对 Data ID 并校验绑定后只探测已运行服务的 loopback readyz，不构造第二个 Deer、数据库池或业务工作器。探针超时 6s；Compose 给它 8s。任一文档不可读或无效时探针失败。

```sh
make compose-up
docker compose --env-file .cache/compose/compose.env ps
docker compose --env-file .cache/compose/compose.env exec -T strategy /readycheck
```

显式迁移和 coreinit 默认同样读取 Nacos。`migrate -apply` 仍保留防误执行开关；历史测试使用的维护工具 `-config` 仅作兼容，不存在于核心服务入口。tools profile 的一次性维护容器只连接 infra 网络，避免与正在运行的核心容器争用固定 IP。

`make core-test` 为每个真实进程发布独立临时 YAML 配对，再无参数启动核心二进制；停止后删除两项，测试命名空间与持久配置隔离。镜像测试同样使用真实 Nacos 双文档，没有本地配置挂载或启动参数，不部署额外基础服务。历史测试/维护的显式统一 JSON 输入仍只用于旧入口兼容，核心启动没有此回退。
