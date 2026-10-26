# ADR 0003：有上下文与实例所有权的 Nacos Naming

日期：2026-10-26。状态：P02.7.1–P02.7.2 已验证认证/查询、注册/心跳/注销与两个 Deer 节点调用；业务进程和生产故障矩阵仍待交付。

Nacos 继续承担注册发现，业务传输继续使用 Kratos/gRPC。P01 的 SDK fake 是版本/参数契约证据，没有验证真实客户端的取消、实例隔离或关闭。

核对 nacos-sdk-go v2.3.5 的 NamingClient、全局 logger，以及已固定的 Kratos Nacos adapter：命名调用不接收调用方 context，adapter 的注册/查询忽略 context；logger 为进程全局，默认文件 writer 的生命周期也不归单个 Deer 拥有。直接包装超时而让调用在后台继续，不能满足当前所有权约定。未在此引入大型 SDK fork、全局 logger 切换或取消测试忽略项。

采用标准 net/http 实现所需的小范围 Naming OpenAPI 适配，明确请求截止、并发配额、typed JSON 投影、认证缓存、响应大小以及终态关闭。已实现登录/查询、ephemeral 注册/注销与 light heartbeat；light 请求不携带完整 beat 注册载荷，服务端报告实例丢失时由 Deer 停止，不隐式重建记录。SDK/adapter 依赖暂保留用于原 P01 契约回归，不将其作为运行时工厂。

测试基线固定 Nacos 2.5.2。根据该版本默认鉴权源码及实连，使用 Authorization Bearer 头携带 token，避免放入查询 URL；只向配置中的地址发请求，不使用环境代理或跟随重定向。命名空间/分组/集群显式配置，实际查询不使用本地实例快照。HTTP 注册响应的 data 在此版本为字符串 `ok`，与手册的 boolean 描述不同，后续生产适配以固定源码和实连断言为准。

代价：需要维护这一有限协议面、认证更新及心跳/发现生命周期；没有 SDK 的 push/cache 容灾能力，也不能据此宣称支持其他 Nacos 主版本。HTTP 实例身份不包含 Deer metadata ID，不能条件删除或 fencing；并发 publisher 必须使用不同发布端点。生产 TLS、账户 ACL、集群与故障恢复需单独验收；注册在线状态始终不代替数据库租约或最终执行 fencing。

来源：[Naming OpenAPI](https://nacos.io/docs/v2.5/manual/user/open-api/)、[2.5.2 鉴权实现](https://github.com/alibaba/nacos/blob/2.5.2/plugin-default-impl/nacos-default-auth-plugin/src/main/java/com/alibaba/nacos/plugin/auth/impl/NacosAuthManager.java)、[2.5.2 实例控制器](https://github.com/alibaba/nacos/blob/2.5.2/naming/src/main/java/com/alibaba/nacos/naming/controllers/v2/InstanceControllerV2.java)、[Go SDK NamingClient](https://github.com/nacos-group/nacos-sdk-go/blob/v2.3.5/clients/naming_client/naming_client.go)。
