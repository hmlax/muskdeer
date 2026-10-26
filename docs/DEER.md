# deer 公共模块与服务封装规范

## 命名与服务形态（已确认）

公共目录与 Go 包名统一 deer，公共 Proto 位于 deer/proto。所有 Kratos 业务服务分别使用统一的 deer.Deer 封装，由 deer.NewDeer 构造；market、strategy、execution、portfolio、cron 保持各自独立进程与服务身份。用户已明确“各业务服务分别使用统一 Deer 封装”。
api 用作平台管理界面，包含可操作的管理前端页面和配套后端接口。API 服务使用 deer.DeerApi，复用 Deer 公共装配，并承载 Hertz HTTP 入口。名称按用户要求保留 DeerApi 的拼写；本规范配套采用 deer.NewDeerApi 作为构造函数。

## Deer 与 NewDeer

目标职责：Deer 统一装配 Kratos 应用生命周期、按角色启用的 gRPC Server、Nacos 注册发现、日志、健康检查及公共依赖。业务服务通过注册自身 RPC/生命周期任务接入统一封装；cron 通过同一封装托管任务生命周期，继续保持独立进程和幂等要求。
Deer 按配置提供 DB、Redis、JetStream、Logger、Metrics、Discovery、gRPC ClientFactory、Health；配置通过 deer/proto/config 定义。serviceSpec 声明服务身份及必需/可选能力；每个进程实例拥有自己的运行状态、连接和清理责任，服务名、实例 ID、端口与 Nacos 注册信息分别配置。
必需初始化失败按逆序释放已建立资源并返回原始错误；cleanup 可重复调用，优雅停止有超时。业务模块接收窄接口，业务持仓、策略调度和交易逻辑由各自模块实现。不得通过 getter 偷偷初始化网络连接。

## DeerApi 与 NewDeerApi

目标职责：DeerApi 组合并复用一个 Deer，增加 Hertz Server、路由/handler 注册、认证授权、request_id、统一响应与错误映射、OpenAPI 接入；通过 Deer 提供的 gRPC 客户端访问各业务服务。
api 服务基于该封装托管管理前端构建产物及 /api/v1 管理接口，页面与接口调用按 R08/A27 交付。DeerApi 负责公共 HTTP 生命周期，具体管理页面和业务路由由 api 服务注册；当前通用封装的启停通过不代表管理界面已实现。
Hertz Server 适配后进入所复用 Deer 的 Kratos 生命周期，统一启动、readiness、信号处理、停止接收请求、等待在途请求及依赖清理；API 进程内公共依赖只初始化一次。DeerApi 初始化失败须清理已经成功构造的 Deer 和 HTTP 资源，避免重复关闭或资源泄漏。
API handler 负责协议边界和调用编排，交易、风控及持仓规则在对应业务服务执行。P02 分步落实这些职责，并通过 A02/A26；实际实现范围以下节及验证记录为准。

## 已实现的构造与运行契约

P02.2 已实现以下 Go 接口，配置类型为 deer/proto/config/v1.ServiceConfig：

```go
deer.NewDeer(ctx, cfg, deer.ServiceSpec) (*deer.Deer, deer.Cleanup, error)
deer.NewDeerApi(ctx, cfg, deer.APISpec) (*deer.DeerApi, deer.Cleanup, error)
```

ServiceSpec.Resources 声明进程拥有的依赖；RegisterGRPC(ctx, owner, registrar) 接收构造上下文、当前 *Deer 和 grpc.ServiceRegistrar，并返回注册错误。APISpec.Service 复用同一个 ServiceSpec，Routes(ctx, owner, hertz) 同步注册 Hertz 中间件与路由。两种回调在公共依赖初始化后执行，可通过 owner 获取借用的依赖；不得自行运行服务、保留 server 后续修改，或追加独立生命周期 hook。构造上下文只用于装配，正常请求 handler 使用请求自身上下文。

P02.4 在开发中的 Go 装配回调新增 ctx/owner 参数，仓库内调用点同步更新；NewDeer/NewDeerApi 构造函数签名及业务 Proto/RPC 线协议不因此改变。已引用旧回调类型的调用方需按上述签名更新。

ServiceConfig 显式提供 name、instance_id、version、startup_timeout、shutdown_timeout 及可选 grpc/http listener。每个 listener 提供 address 和 request_timeout；请求超时必须不超过停止预算。NewDeer 不接受 HTTP 配置，NewDeerApi 必须配置 HTTP；gRPC 可关闭。构造时复制配置，校验后才获取依赖并预留监听端口；端口占用或回调失败均回滚。

gRPC unary 由 Kratos 添加服务端请求超时；stream（包括 health Watch）要求客户端原生 deadline 且不超过配置 request_timeout，否则 InvalidArgument。原生 deadline 可中断阻塞的流读写；只包装 handler 上下文不能保证中断 RecvMsg/SendMsg。

调用方构造成功后应立即安排返回的 Cleanup。Run(ctx) 至多调用一次，阻塞到上下文取消、cleanup、进程信号或监听失败；返回前停止两种 transport，再逆序释放资源。上下文取消通过 errors.Is(err, context.Canceled/DeadlineExceeded) 识别；正常 cleanup 停止成功返回 nil。cleanup 可在 Run 前、运行中或返回后调用，重复/并发调用只清理一次。清理使用配置中的独立超时上下文，资源和 handler 必须遵守上下文，不能强制终止不合作的 Go 函数。

Ready/WaitReady 表示本实例全部配置 listener 已开始 Accept 且尚未停止；启动就绪前再次检查已配置的 PostgreSQL/Redis/JetStream/Nacos。运行中的健康入口应调用 CheckReady(ctx)，同时检查生命周期与当前依赖连通性；Ready() 本身是生命周期状态，不能代替实时依赖检查。GRPCAddress/HTTPAddress 返回实际监听地址（测试可使用端口 0）。后续业务风险、对账、租约健康仍须单独接入。Hertz 采用显式标准网络 transport，逐响应设置 Date，避免默认日期 ticker；未启用带进程级 poller 的即时断连感知，请求取消目前依靠显式 deadline，不能据此宣称实时感知客户端断开。

实际证据见 [P02.2](validation/P02-02.md)。P02.3–P02.7.2 已接入日志、PostgreSQL/Redis、JetStream 同步发布、Nacos 查询与注册/心跳/注销，验证两个 Deer 节点的发现调用；gRPC ClientFactory、进程入口日志桥接、指标、API 鉴权/统一响应/OpenAPI 及六个业务进程尚未交付；不将完整 A02/A26 或 P02 标完成。DeerApi/NewDeerApi 拼写按用户指令保留，对这两处使用精确的 revive 命名规则例外，其余规则仍运行。

## 已实现的资源所有权基础

P02.1 已实现 Cleanup(ctx) error 与 Initializer{Name, Open}：资源获取前校验计划，失败时逆序释放全部已获取资源，保留原始初始化错误与释放错误；部分初始化失败也须返回 cleanup。清理同步执行，具有独立超时上下文，重复/并发调用只释放一次。资源回调须遵守上下文并负责自己创建的连接或 goroutine。
两个服务构造函数已在 P02.2 接入该基础；所有权层的原始范围和证据见 [P02.1](validation/P02-01.md)。

## PostgreSQL/GORM
连接数、空闲数、最大生命周期、语句超时、连接超时显式配置；context 贯穿；事务由调用方拥有。支持明确事务回调，不通过全局变量切换 DB。空值和 Select 更新语义必须验证；禁止 Updates(struct) 无意忽略 0/false。
所有数据操作显式以 db.WithContext(ctx).Model(&model.X{}) 绑定生成模型，事务内使用 tx.WithContext(ctx).Model(&model.X{})；禁止任何 DB 变量上的 .Table(...)，公共组件不得提供接收表名的绕过接口。每张表必备 Id 自增主键、createAt/updateAt NOT NULL、deleteAt 可为 NULL，字段映射及生成要求见 PROTO_AND_MODEL.md。

P02.4 实现 deer/postgres.Client：Postgres Proto 显式提供 TCP host/port、database、username、password_env、池参数及四种超时；密码只在构造时从环境变量引用读取，不保存在配置 Proto，运行中不自动重载。ssl_mode 缺省 verify-full，也支持显式 require/disable；root_cert_file 可配置。schema 缺省 public，只接受最多 63 字节的小写 ASCII 标识符，通过连接 search_path 指定，业务数据访问继续使用生成 Model。

max_open_conns 为 1..1024，max_idle_conns 为 0..max_open_conns；max_lifetime、max_idle_time、connect_timeout 必须为正且可精确表示为 Go duration。statement_timeout 必须为 1..2147483647 的整数毫秒；会话固定 UTC。连接池显式 PingContext，GORM 禁用无上下文自动 ping；不自动迁移。GORM 不打印 SQL/参数，查询错误交回调用边界处理。

NewDeer 在日志之后、业务依赖之前创建配置中的 PostgreSQL，停止时逆序释放。DB() 返回借用的 *gorm.DB；未配置时返回 ErrPostgresNotConfigured，注册回调可据此拒绝缺少必要依赖的服务。Client.Ping/Stats 可用于活性检查与指标，Client.Close 幂等，服务拥有池生命周期。连接错误的文本不输出 DSN/密码，原始原因通过 errors.Is/As 保留。证据见 [P02.4](validation/P02-04.md)。

## Redis / NATS
Redis 键包含环境、服务、资源类型，TTL 显式。NATS subject 区分环境与事件版本；发布带 event_id；JetStream consumer durable、AckExplicit、重试上限和人工恢复规则有配置。达到 MaxDeliver 的事件要有可观测的隔离/重投流程，不假设自动存在 DLQ。

P02.5 实现 deer/redis.Client 作为可重建的 standalone 字符串缓存，不能充当权威账本或执行所有权。Redis Proto 显式配置 address（TCP host:port）、非负 database、可选 username/password_env、key_prefix、pool_size 及五种正 duration。key_prefix 至少含环境和服务两段，例如 `development:market`，只含 ASCII 字母、数字、点、下划线、连字符及分隔冒号；各段非空，总长不超过 128。业务传入 `quote:BTC-USDT` 等带资源类型的相对键，实际键为 `<prefix>:<relative>`；副本共享前缀，禁止将 instance_id 加入共享缓存前缀。

New 在构造时复制配置、解析密码引用并有界 PING。TLS 缺省开启，验证主机名，最低 TLS 1.2，可指定 root_cert_file；仅隔离开发环境显式 tls_enabled=false。pool_size 为 1..1024，同时设置 MaxActiveConns/MaxConcurrentDials；禁用额外 pipeline pool、命令重试、重复 dial 尝试、维护通知和身份协商。connect_timeout、command_timeout、pool_timeout、max_idle_time、max_lifetime 均须为正且能精确表示为 Go duration。

Get/Set/Delete/Ping 接收 context，命令使用调用方与 command_timeout 中较早的 deadline；已取消上下文不会发命令。网络 I/O 中途取消的响应仍受已设置的 socket deadline 约束，不能承诺无 deadline 的取消即时中断。Set 的 TTL 必须为正整数毫秒；值为 string，小数不经浮点转换。Get 缺失返回 ErrNotFound；SET 超时代表结果未知，调用方不能据此认定未写入，本层不重放命令。错误文本隐藏地址/口令，原始原因保留给 errors.Is/As。

Deer 在 PostgreSQL 之后、业务依赖之前初始化 Redis，停止时逆序关闭；Redis() 返回借用客户端，未配置返回 ErrRedisNotConfigured。CheckReady 和启动 readiness 包含实际 PING。Close 幂等且关闭全部池和连接；SDK 失败 dial 的辅助恢复探针可能在 Close 后等待一个连接超时及一秒定时器才退出，不重放业务命令。单连接失败用虚拟时间验证有界退出，未添加 goleak 忽略项；此范围及真实 Redis 证据见 [P02.5](validation/P02-05.md)。Cluster/Sentinel、PubSub、锁和权威数据接口未开放。

P02.6 实现 deer/jetstream.Client 与借用的 Deer.JetStream()，位于 Redis 之后初始化并逆序关闭；未配置返回 ErrJetStreamNotConfigured。JetStream Proto 指定单一 TCP address、已部署的 stream、`muskdeer.<environment>.<service>` subject_prefix、可选 token_env、connect_timeout/command_timeout、max_inflight（1..1024）、max_message_bytes（1..1048576）及默认开启的 TLS/可选根证书。TLS 验证主机名且最低 1.2；不在配置中存放 token。构造复制配置，TCP/握手共用调用方与 connect_timeout 的较早期限；取消关闭初始 socket，连接完成后移除构造上下文引用。

应用不创建/修改 stream。构造及健康检查读取实际流配置，要求允许 ACK、未 sealed，且 Subjects 明确包含 `<subject_prefix>.>`；共享流应列出各服务的此类前缀，只有祖先通配符的配置会被拒绝。此检查不替代 broker 账户 ACL、持久化副本和容量配置。

Publish(ctx, suffix, eventID, proto.Message) 有界准入、确定性 Protobuf 编码并同步等待存储确认；suffix 如 `order.state.v1`，eventID 为最多 64 字节 ASCII 字母/数字/点/下划线/连字符，须在整个 stream 内稳定且唯一。设置 Nats-Msg-Id/预期 stream，返回 Stream/Sequence/Duplicate；消息借用至调用返回，不能并发修改。当前仅用技术 Probe 验证类型/精确字符串；P03 才定义业务 envelope 和明确 payload，不用通用发布器冒充业务契约。

禁用 SDK 发布重试、重连缓存、自动重连及发现到的其他服务器；首版连接断开后安全拒绝，需重建客户端/重启进程。该恢复监督将在后续服务恢复步骤接入，当前不宣称自动恢复或 HA。命令期限覆盖准入及等待 ACK；超时可能已落盘，重试由持久化 outbox 的所有者决定。Broker 去重只作用于有限窗口，同 ID 不会验证 payload 指纹，不能替代数据库 inbox/成交唯一键；业务持久化指纹、事务及重投在 P03/P04 完成。

Close 先拒绝准入，关闭连接并等待终态回调，重复关闭幂等；已确认发布已落盘，进行中的发布仍可能不确定。未开放 consumer、异步发布或无范围的 SDK 句柄，不调用异步 Drain 来暗示未知发布已获确认。真实 ACK 丢失/去重、命名空间、失败清理等证据见 [P02.6](validation/P02-06.md)；consumer AckExplicit/MaxDeliver、隔离恢复、outbox/inbox 和 broker 集群故障验收尚未完成。

## gRPC / Nacos
服务名、namespace、group、实例 ID、端口、健康与注销规则统一。gRPC 客户端设置截止时间；只对已定义幂等操作重试。启动注册必须晚于依赖就绪。健康区分进程存活与可服务；租约丢失不应继续报告“可执行交易”。

`grpc_security` 为 gRPC server 配置双向 TLS：`mtls.certificate_file/private_key_file/root_cert_file` 引用独立提供的 PEM 文件，`allowed_peer_services` 明确允许的调用方服务 DNS SAN，例如 `api.grpc`。证书必须包含本服务 `<name>.grpc` 的精确 SAN，服务器证书在构造时须处于有效期。使用 TLS 1.3、受信 CA 链及客户端证书，额外匹配许可中的精确 SAN，拒绝通配许可；请求 header/metadata 不能替代证书身份。未配置时仅允许字面量 loopback 监听，非 loopback 必须启用 mTLS；此例外只供隔离本地测试。

`deer/rpcsecurity.Client` 加载调用方的精确服务证书，按受信 CA 与期望被调服务的精确 DNS SAN 验证对端，身份不由 Nacos 返回的 IP 决定。加载的配置为实例独立快照，没有全局证书状态。证书轮换当前需要重建实例，未实现热加载；业务用户权限、最终执行 fencing、运行中证书到期的健康/告警仍须分别接入。过程身份许可不等同于管理用户权限。实连安全边界见 [P02.7.3](validation/P02-07-03.md)。

P02.7.1 实现基于固定 Nacos 2.5.2 OpenAPI 的 deer/nacos.Client，原因见 [ADR 0003](decisions/0003-context-bound-naming.md)。配置显式指定 address（TCP host:port）、私有 namespace_id、group、cluster、username/password_env、两类正 duration 及 max_inflight 1..64；TLS 默认开启，可设根证书，禁止 URL 内嵌身份。客户端不读环境代理、不跟随重定向；认证使用 Bearer header，不将 token 放入 URL，密码只在登录表单。token 缓存至服务端有效期的 80%，之后由下次操作刷新；失效请求返回错误，下一请求再认证，不在失败操作内重放。

Deer 在 JetStream 后初始化，Discovery() 返回借用客户端，缺失配置返回 ErrNacosNotConfigured；getter 不发请求。GetService(ctx, "market.grpc") 每次读实际 API，返回 typed Instance，不保留本地实例快照；过滤不健康/disabled/零权重/其他集群及协议。Nacos 既有数字权重以 json.Number 接收后直接转 decimal.Decimal，仅用于过滤，不进入业务协议或浮点中转。身份重复、无效地址、畸形/超大响应安全拒绝。并发准入和连接受配额限制，响应上限 1 MiB/1024 实例，期限涵盖等待、认证与读取。Close 撤销准入、取消并 join 请求后释放连接，API 装配失败也逆序清理。

启动及实时健康会在同一 scope 查询 `deer-readiness.grpc`，账户需有该 READ 权限，不要求存在服务提供者；namespace 由部署方预建。认证/查询证据见 [P02.7.1](validation/P02-07-01.md)；测试管理员样例不作为生产 ACL/TLS 或跨版本证据。

可选 `nacos.registration` 启用本服务 gRPC 地址发布，必须同时配置 gRPC listener。`advertise_ip` 为明确的单播地址；端口使用实际预留端口，不能把监听通配地址当作发布地址。`heartbeat_interval` 为 50–5000 整毫秒。未配置 registration 的出站消费者保持仅查询能力。服务名为 `<name>.grpc`，metadata 包含 Deer instance_id、version 与协议。

Deer 在 listener 接受连接、公共依赖通过健康检查后注册 ephemeral 实例，成功才就绪；受实例拥有的循环发送 light heartbeat，启动 context 不保留为后台所有权。心跳失败即撤销就绪并终止 Run，保留错误，不自动重试注册或重建丢失记录。Health 检查心跳新鲜度；正常停止先撤销就绪、取消并 join 心跳、注销，再停止 transport/公共依赖。注册超时结果可能未知，启动回滚也尝试注销；未启动的实例不发注销请求。

Nacos HTTP 实例身份是 namespace/group/service/IP/port/cluster，Deer ID 仅为 metadata，不提供条件删除或 fencing。并发 publisher 必须使用不同发布端点，不能把重用端口当作所有权切换协议。注册在线或心跳成功都不授权交易，最终执行仍须验证后续数据库租约/epoch。真实两节点和外部删除测试见 [P02.7.2](validation/P02-07-02.md)，目前是同一测试进程内的独立 Deer 实例；独立业务进程、watch/gRPC ClientFactory、生产 ACL/TLS 与完整故障恢复矩阵仍待后续验证。

## API 统一返回
JSON: {code, message, data, request_id}。分页 data: {items, next_cursor}；cursor 不暴露数据库内部细节。成功 200/201；异步操作 202 带 operation_id；参数 400，未认证 401，无权限 403，不存在 404，版本冲突 409，限流 429，依赖不可用 503。禁止所有错误都 HTTP 200。
业务错误映射 gRPC status 及 HTTP；内部日志记录根因，前端返回安全信息。request_id 通过 middleware/interceptor 传递；未知传入值校验长度。

## 日志
Kratos log 接口由结构化日志后端适配，lumberjack 实现文件大小轮转。配置 filename、max_size_mb、max_backups、max_age_days、compress、stdout；每节点独立路径。字段包含 service、instance、request_id、strategy_id、operation、error_code；敏感信息脱敏。

P02.3 已实现 deer/logging，并由 NewDeer 在所有业务依赖之前创建、最后关闭；DeerApi 复用该实例。ServiceConfig.logging 缺省 stdout INFO，显式 Logging 需启用 stdout 或提供完整 LogFile。文件配置 filename 为基路径，实际文件为 `<目录>/<service>/<instance>/<文件名>`；独占锁拒绝相同文件的并发写入。ServiceSpec.LogOutput 可覆盖借用的 stdout 输出，实例清理不关闭该输出。

Deer.Logger() 返回借用的日志实例；LogAttrs 返回写错误，Slog() 适配标准 API，Log 实现 Kratos Logger 接口。调用者不得自行关闭服务拥有的 logger。级别支持 debug/info/warn/error；文件大小 1..1024 MiB，max_backups 为正，max_age_days 为 1..36500。配置在构造时复制，运行中不修改。

使用有来源记录的同步 lumberjack 维护版本，保持单 go.mod 和完整 race/goleak 检查；选择原因及同步 I/O 代价见 [ADR 0002](decisions/0002-owned-logging.md)，验证见 [P02.3](validation/P02-03.md)。结构化敏感 key/group 脱敏，decimal 输出 string，任意对象不直接写入日志；固定消息与错误文本仍须由调用者保证不含秘密。自动请求字段注入和各业务进程入口的框架日志桥接仍待接入。

## 可观测性
/metrics 与健康端口走内网授权。指标包括行情年龄、报价偏离、gRPC 延迟、订单 UNKNOWN 数、撤单延迟、对冲积压、租约丢失、outbox 年龄。订单/成交 ID 只进日志，不进指标 label。存活检查不因短暂依赖故障反复杀进程；readiness 体现可服务状态。
