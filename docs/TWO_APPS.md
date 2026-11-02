# 两应用运行说明

2026-11-02 用户要求：React 前端 + operator（Kratos/gRPC）+ admin（Hertz/HTTP），保留单仓库、单 go.mod。

## 构建与启动

### Docker Compose 连接既有 infra

`compose.yml` 只部署 admin/operator，连接已存在的 `infra-network`；PostgreSQL、Redis、Nacos 容器分别默认为 infra-postgres、infra-redis、infra-nacos。不会运行仓库中已停用的 compose.dev.yml/compose.test.yml，不创建或停止任何基础服务。准备脚本需要 Python 3.11+、Docker Compose、OpenSSL；前端使用项目固定工具链。

```sh
make compose-up
docker compose --env-file .cache/compose/compose.env ps
```

首次准备从 `.cache/infra/nacos-credentials.json` 读取既有 Nacos 登录（username/password），从既有容器读取 PostgreSQL/Redis 本地凭据。仅创建项目专属 PostgreSQL 数据库/角色 muskdeer_local、Nacos 命名空间 muskdeer-local、本地 mTLS CA/服务证书和随机管理密码；本地文件被 Git 忽略，权限 0600。数据库/角色以随机所有权标记识别，拒绝接管无标记的同名资源，重复准备不重置密码。

镜像内的 `/migrate` 与应用来自同一 Go 1.27.3 源码；启动前显式执行迁移，再用 `/adminuser` 初始化一次本地管理员，服务自身不迁移或开户。浏览器地址为 **http://127.0.0.1:59000**，用户名 admin，随机密码在 `.cache/compose/credentials.json` 的 password 字段。该文件还含本地数据库密码，请勿提交或分享。

当前启动管理查询/控制模块，不配置交易账户、资金、行情工作循环或策略执行者，因此新数据库页面为空。没有新增风险或实际下单；消息发布/消费尚未启用，不需要 NATS 流。后续启用交易消息时继续使用既有 infra-nats，需独立持久开发账户及预置流，不能复用会被测试清理的 muskdeer-test 账户。

admin 的 `allow_container_listener` 仅显式允许本地 Docker 的 0.0.0.0 监听，public_origin 仍必须固定为 literal-loopback HTTP；宿主发布端口只绑定 127.0.0.1。operator 不发布宿主端口，admin 通过 Nacos 发现并使用 mTLS 调用 operator.grpc。应用内部网络固定 172.31.240.0/24（operator .10），准备阶段拒绝与其他 Docker 网络重叠；基础服务仍走外部 infra-network。运行用户为准备者的非 root UID/GID，使只读挂载的 0600 配置/密钥可读；每个容器只挂载自己的配置和服务私钥，CA 私钥不挂载。容器只读、移除 capabilities，并限制日志轮转、内存和进程数。

```sh
# 已完成准备、构建、迁移和账户初始化后的再次启动
docker compose --env-file .cache/compose/compose.env up -d --wait
# 仅停止/移除这两个应用，持久数据和既有 infra 保留
make compose-down
```

本次工作区保留未完成的平仓修改，运行镜像由已提交版本加本步修改的独立源码构建（schema 37），源码保存在被忽略的 `.cache/compose/source`，具体路径见 compose.env。普通干净检出默认直接构建仓库；要明确改用当前工作区，可运行 `python3 scripts/compose.py prepare --source .` 后重新构建。不要让未提交 schema 38 与 schema 37 运行镜像混用。证书为本地开发用途，有效期不足一天时准备脚本拒绝继续，需明确轮换后重建/重启应用。

### 原生构建

使用 Go 1.27.3，工具测试和前端工具脚本需 Python 3.11 或以上。运行 `make build`，产物为 `.cache/bin/operator`、`.cache/bin/admin`，以及独立工具 migrate/adminuser。初始化账户命令由原 admin 更名为 adminuser，避免与 HTTP 应用混淆。

```sh
.cache/bin/operator -config configs/local/operator.json
.cache/bin/admin -config configs/local/admin.json
```

本地样例默认不启用数据库业务或管理登录；禁止用启动成功当作交易功能已启用。`make dev-up` 复用现有 Docker infra，为本工作区生成 `.cache/dev/operator.json`、`.cache/dev/admin.json`。开发数据库配置使用工作区专属 schema，避免向其他项目的 public schema 写表；先显式创建此 schema，再执行 migrate。显式开启业务前，按原业务合同准备迁移、账户、行情绑定、资金、预算和消息流；服务不自动迁移或开户。

operator 的 BusinessRuntime 复用现有 Proto 字段，在一个 Deer 中装配 market、execution、strategy、portfolio 模块。配置 `strategy.management_enabled` 开启管理读取、`control_enabled` 开启写入；`strategy.account_ids`、`market.feeds`、`risk`、`simulator`、`portfolio_consumer` 分别选择现有资源。执行或投影模块需要 JetStream；账户工作循环需要 operator 的出站 gRPC 客户端。所有模块共享数据库池、消息客户端、日志和生命周期。定期任务将归同一应用，尚未实现的 P09 任务不因合并进程自动完成。

Nacos 仅注册 operator.grpc；operator 服务端证书和内部客户端证书使用 operator.grpc SAN，允许 admin.grpc 和 operator.grpc。admin 出站证书使用 admin.grpc SAN，allowed_services 仅配置 operator.grpc；admin 的 admin_http 开启管理页面/查询/控制，gRPC 请求仍有原生超时。执行命令按账户当前 owner 的实例 ID 路由；迁移为同一服务名不取消 owner 路由、租约、风险检查和最终 fencing。

`make two-apps-test` 复用现有 infra-postgres/infra-redis/infra-nacos/infra-nats，创建并清理独立数据库、命名空间及测试账户中的消息资源，验证真实 operator 模块和 admin HTTP→gRPC 闭环；不启动或删除基础服务容器。`make image` 构建独立 muskdeer-admin:local 与 muskdeer-operator:local；`make image-test` 对两个非 root、只读镜像验证启动/readiness/JSON 日志/SIGTERM。

每个服务有独立入口：`cmd/admin` 与 `cmd/operator` 各自解析参数；不使用 role 参数，共用 `cmd/muskdeer` 已删除。历史模块回归分别使用 `internal/testservice/{api,market,strategy,execution,portfolio,cron}` 的独立入口，由 `make build-fixtures` 构建，每个夹具都有自己的可执行文件。这些仅供历史模块隔离回归，不属于部署应用。

## 权限迁移

admin 校验登录、会话、角色和 CSRF，特别是风险恢复要求 admin 角色。通过已认证 admin.grpc 的 mTLS 连接发送 user/session ID、用户名和角色审计快照；业务 RPC 不携带会话 token 或 CSRF。operator 校验服务身份与审计字段形状，不重新读取会话或检查管理角色，继续执行业务版本、幂等、资金和最终 fencing。授权以 admin 接受请求时的快照为准；不再承诺浏览器注销与业务写入提交之间的事务原子性。旧模块夹具仍保留历史会话合同，生产 operator 的注册边界不接受该入口。

## 复用现有 infra

测试容器名默认 infra-postgres、infra-redis、infra-nacos、infra-nats；可用 MUSKDEER_INFRA_*_CONTAINER 指定已有容器。脚本只 inspect/exec，使用发布端口访问 loopback，不执行 compose up/down 或创建基础服务。

PostgreSQL 使用每次运行唯一的 muskdeer_test_* 数据库，Nacos 使用两份唯一命名空间，结束清理；Nacos 的认证拒绝用例使用临时无管理员角色的账号并等候缓存生效，结束删除。Redis 使用每次运行唯一的项目测试键前缀；超时/连接池测试只延迟测试 TCP 连接的响应，不暂停共享 Redis。NATS 需独立 muskdeer-test 账号及 JetStream 空间，避免接触其他应用的流。服务的客户端支持 token 或 username/password_env 二选一；密码只从环境变量读取。测试入口串行锁定本项目测试，清理仅覆盖独立测试账户中 muskdeer.* 流。

Nacos 凭据通过 MUSKDEER_INFRA_NACOS_USERNAME/PASSWORD 或被忽略的 .cache/infra/nacos-credentials.json（username/password）提供；NATS 同理使用 MUSKDEER_INFRA_NATS_USERNAME/PASSWORD 或 .cache/infra/nats-test-credentials.json。禁止提交这些本地文件。脚本不会初始化管理员或重置已有凭据。CI 集成和浏览器任务需已有 infra 的 self-hosted、muskdeer-infra 标签 runner，普通 checks 不部署基础服务。
