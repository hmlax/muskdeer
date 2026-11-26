# Deer 公共运行层

2026-11-04 用户要求服务代码归各自项目，直接移除 operator/API。DeerApi、NewDeerApi 和 APISpec 已删除，替代此前保留 API 通用封装的约定。market、strategy、execution、portfolio、cron 各有独立 cmd/internal/app/Wire，每进程仅一个 Deer。默认 Compose 运行四个交易核心；cron 当前为健康/生命周期基础进程，没有定期业务。

## 构造与所有权

`deer.NewDeer(ctx, cfg, spec, options...)` 返回 `(*Deer, Cleanup, error)`。Cleanup 接收 context 并返回 error，必须在成功构造后立即安排清理；Run 同样在返回前排空和释放资源。清理幂等，错误合并返回。构造失败逆序回滚已取得的资源和监听。

ServiceSpec.Resources 声明公共依赖就绪后初始化的进程资源；Workers 同步描述阻塞工作计划，RegisterGRPC 同步注册本服务协议。回调不得启动无所有者 goroutine、创建第二份连接池或保留 registrar 延迟修改。各服务通过所属 business 包装配，公共运行层不按服务名选择业务。

进程按实际配置的策略实例和数据依赖装配工作器，不设置策略数、账户数或工作器总数的固定上限，不截断数据库记录。资源并发和连接池只负责调度；正常关闭仍必须等待所有已装配工作器退出并释放依赖。

Deer 默认按配置创建并拥有 PostgreSQL、Redis、JetStream、Nacos、RPC 客户端和日志。WithDB、WithRedis、WithJetStream、WithDiscovery、WithGRPCClients、WithLogger 注入的对象为借用，不重复创建或关闭；调用方在 Deer 完成后关闭。初始化、启动和运行 readiness 均检查配置/注入依赖。

ServiceConfig 只是内存装配结果。进程从 env 引导 Nacos，严格读取 data.yaml 与 service.yaml 的强类型配对配置，校验身份、版本及内容哈希；不回退本地文件、不热切换业务。详见 [Nacos 配置](NACOS_CONFIG.md)。配置在取得资源前校验并复制；`http`、`admin_http` 已拒绝。

## 生命周期与工作器

Run 只可调用一次，启动 Kratos 监听和有明确所有者的工作器。所有任务必须响应 context，退出后才关闭借用资源。工作器的 Run、Check 和 Shutdown 合同及有界轮询保持；构造不等于开始工作。进程桥接 Kratos/Hertz/SDK 日志后，在依赖清理完成、日志关闭前恢复原全局 logger。

Ready/WaitReady 表示监听开始接受连接且尚未停止；CheckReady(ctx) 同时检查当前公共依赖、注册发布和工作器。依赖失效影响 readiness，Live 只表示进程仍拥有生命周期。GRPCAddress 和 MonitoringAddress 返回实际地址，测试支持端口 0。

## gRPC 与本地监控

所有五个服务必须配置 gRPC。Deer 自动注册标准 `grpc.health.v1.Health/Check`、`List`、`Watch`；service 可为空或 `<name>.grpc`，Check 对未知名称返回 NotFound。Watch 有容量和调用期限限制。健康服务与业务共用认证监听，不绕过 mTLS。SERVING 是运行就绪信号，不能代替行情可靠、风险、租约或对账门禁。

非 loopback gRPC 必须双向 TLS，校验受信 CA、有效证书和服务 DNS SAN 许可；只允许本地测试使用字面量 loopback 明文。gRPC 客户端通过 Nacos 按服务合并刷新健康节点，复用同一服务/实例/版本/地址的长连接；节点移除或发现失败使旧通道退役，指定实例不回退其他 owner，不把 execution/market 请求改投 operator；断线重连不重放已发送的业务请求。方法级业务身份校验继续保留。

Hertz 只保留公共 loopback 监控：`/livez`、`/readyz`、`/metrics`。它与 gRPC 同属 Deer/Kratos 的启动、停止、排空与失败回滚。没有管理 HTTP 封装或公开路由装配入口。监控响应有 deadline、请求 ID 和有界 probe 指标；metrics 抓取不统计自身。测试使用私有合成路由验证公共传输排空和请求关联，不构成可运行的 API 服务。

## 存储、消息与日志

PostgreSQL 使用 GORM，所有数据操作显式 WithContext/Model，Proto 是持久化结构唯一来源；启动不 AutoMigrate。Redis 是辅助缓存。JetStream 负责可靠投递，业务去重、资金预留、outbox/inbox 事务和最终 fencing 继续由共享交易合同及事务仓储保证。持久 claim 类型、错误和 subject 在 deer/trading/outboxcontract，执行工作器在 execution/outbox。

每个实例独立 JSON 日志及按大小轮转文件，日志/指标不含凭据、完整敏感请求或订单 ID 高基数标签。公共组件与服务归属见 [目录约定](PROJECT_LAYOUT.md)，生成与清理合同见 [Wire 装配](WIRING.md)。

## RPC连接与发现生命周期（2026-11-26）

Factory首次使用某服务时建立一个独立于请求取消的发现所有者，通常每秒刷新；断线通知合并并限频，最多4个发现请求同时运行，为Nacos其他生命周期操作保留空间。节点列表超过5秒未成功更新时拒绝新调用，发现失败或节点删除/地址与版本变化时退役相应通道。没有回退到过期列表，也不把路由缓存作为资金或执行所有权授权。

通道按服务、实例ID、版本、地址复用，最多对应允许服务的当前有效节点集合，按需创建；空闲1分钟后由gRPC释放底层传输，节点退役则关闭通道。断线由gRPC按有界退避重连，当前已发送调用仍只发送一次；包中的invokeOnce及REFUSED_STREAM回归保持。Factory关闭停止新准入，取消发现、普通调用和订阅，关闭通道并等待所有者退出；发现客户端由外层随后关闭。只读订阅与unary共用通道，订阅不占unary准入位置。
