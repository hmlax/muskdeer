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

ServiceSpec.Resources 声明进程拥有的依赖；RegisterGRPC 接收 grpc.ServiceRegistrar 并返回注册错误。APISpec.Service 复用同一个 ServiceSpec，Routes 同步注册 Hertz 中间件与路由；回调不得自行运行服务、保留 server 后续修改，或追加独立生命周期 hook。正常业务请求 handler 必须遵守上下文。

ServiceConfig 显式提供 name、instance_id、version、startup_timeout、shutdown_timeout 及可选 grpc/http listener。每个 listener 提供 address 和 request_timeout；请求超时必须不超过停止预算。NewDeer 不接受 HTTP 配置，NewDeerApi 必须配置 HTTP；gRPC 可关闭。构造时复制配置，校验后才获取依赖并预留监听端口；端口占用或回调失败均回滚。

gRPC unary 由 Kratos 添加服务端请求超时；stream（包括 health Watch）要求客户端原生 deadline 且不超过配置 request_timeout，否则 InvalidArgument。原生 deadline 可中断阻塞的流读写；只包装 handler 上下文不能保证中断 RecvMsg/SendMsg。

调用方构造成功后应立即安排返回的 Cleanup。Run(ctx) 至多调用一次，阻塞到上下文取消、cleanup、进程信号或监听失败；返回前停止两种 transport，再逆序释放资源。上下文取消通过 errors.Is(err, context.Canceled/DeadlineExceeded) 识别；正常 cleanup 停止成功返回 nil。cleanup 可在 Run 前、运行中或返回后调用，重复/并发调用只清理一次。清理使用配置中的独立超时上下文，资源和 handler 必须遵守上下文，不能强制终止不合作的 Go 函数。

Ready/WaitReady 表示本实例全部配置 listener 已开始 Accept 且尚未停止；GRPCAddress/HTTPAddress 返回实际监听地址（测试可使用端口 0）。当前 readiness 只包含构造依赖和监听状态，后续业务风险、对账、租约健康仍须单独接入。Hertz 采用显式标准网络 transport，逐响应设置 Date，避免默认日期 ticker；未启用带进程级 poller 的即时断连感知，请求取消目前依靠显式 deadline，不能据此宣称实时感知客户端断开。

实际证据见 [P02.2](validation/P02-02.md)。Nacos、真实基础设施工厂、日志轮转、指标、API 鉴权/统一响应/OpenAPI 及六个业务进程尚未交付；不将完整 A02/A26 或 P02 标完成。DeerApi/NewDeerApi 拼写按用户指令保留，对这两处使用精确的 revive 命名规则例外，其余规则仍运行。

## 已实现的资源所有权基础

P02.1 已实现 Cleanup(ctx) error 与 Initializer{Name, Open}：资源获取前校验计划，失败时逆序释放全部已获取资源，保留原始初始化错误与释放错误；部分初始化失败也须返回 cleanup。清理同步执行，具有独立超时上下文，重复/并发调用只释放一次。资源回调须遵守上下文并负责自己创建的连接或 goroutine。
两个服务构造函数已在 P02.2 接入该基础；所有权层的原始范围和证据见 [P02.1](validation/P02-01.md)。

## PostgreSQL/GORM
连接数、空闲数、最大生命周期、语句超时、连接超时显式配置；context 贯穿；事务由调用方拥有。支持明确事务回调，不通过全局变量切换 DB。空值和 Select 更新语义必须验证；禁止 Updates(struct) 无意忽略 0/false。
所有数据操作显式以 db.WithContext(ctx).Model(&model.X{}) 绑定生成模型，事务内使用 tx.WithContext(ctx).Model(&model.X{})；禁止任何 DB 变量上的 .Table(...)，公共组件不得提供接收表名的绕过接口。每张表必备 Id 自增主键、createAt/updateAt NOT NULL、deleteAt 可为 NULL，字段映射及生成要求见 PROTO_AND_MODEL.md。

## Redis / NATS
Redis 键包含环境、服务、资源类型，TTL 显式。NATS subject 区分环境与事件版本；发布带 event_id；JetStream consumer durable、AckExplicit、重试上限和人工恢复规则有配置。达到 MaxDeliver 的事件要有可观测的隔离/重投流程，不假设自动存在 DLQ。

## gRPC / Nacos
服务名、namespace、group、实例 ID、端口、健康与注销规则统一。gRPC 客户端设置截止时间；只对已定义幂等操作重试。启动注册必须晚于依赖就绪。健康区分进程存活与可服务；租约丢失不应继续报告“可执行交易”。

## API 统一返回
JSON: {code, message, data, request_id}。分页 data: {items, next_cursor}；cursor 不暴露数据库内部细节。成功 200/201；异步操作 202 带 operation_id；参数 400，未认证 401，无权限 403，不存在 404，版本冲突 409，限流 429，依赖不可用 503。禁止所有错误都 HTTP 200。
业务错误映射 gRPC status 及 HTTP；内部日志记录根因，前端返回安全信息。request_id 通过 middleware/interceptor 传递；未知传入值校验长度。

## 日志
Kratos log 接口由结构化日志后端适配，lumberjack 实现文件大小轮转。配置 filename、max_size_mb、max_backups、max_age_days、compress、stdout；每节点独立路径。字段包含 service、instance、request_id、strategy_id、operation、error_code；敏感信息脱敏。

## 可观测性
/metrics 与健康端口走内网授权。指标包括行情年龄、报价偏离、gRPC 延迟、订单 UNKNOWN 数、撤单延迟、对冲积压、租约丢失、outbox 年龄。订单/成交 ID 只进日志，不进指标 label。存活检查不因短暂依赖故障反复杀进程；readiness 体现可服务状态。
