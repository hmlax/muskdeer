# 策略管理查询

P07.6 提供 `domain.AdminStrategies` 和借用现有数据库的 `repository.NewAdminStrategies`，P07.7 接入 typed 管理 gRPC，P07.8 通过 API/DeerApi 的 Hertz 提供只读 HTTP，P07.9 交付列表/详情/版本页面。写操作仍未交付，不能据此将 R08/A27 标为完成。身份、查询HTTP和页面入口见 [管理 HTTP](ADMIN_HTTP.md)，运行目标与实际状态见 [生命周期](STRATEGY_LIFECYCLE.md)。

每次 List、Detail、Versions 均在同一个只读 REPEATABLE READ 事务内校验持久会话、用户启用状态、credential_version、数据库时钟到期及 viewer 权限，然后读取业务数据。注销或凭据变更后的新查询不能沿用此前 API 身份检查的结果。查询是快照观测，不是新增交易风险的授权；并发注销不撤回已经开始的只读快照。

List 按实例自增 ID 升序，Versions 限定一个实例并按配置行自增 ID 升序。`after_id` 为排他游标，limit 必须为 1–100；多读一行判断下一页，末页游标为零。实例及配置均排除软删除。分页请求各有快照，不承诺跨多次请求的固定全集。不存在的实例返回明确 not-found，不伪装成空版本列表。

Detail 在同一快照内返回实例、完整请求配置和可为空的实际生效配置。请求来源、实际来源分别来自各自持久身份，不将 OKX 请求显示成已生效，不从状态名推导请求目标。版本中的 Maker/Grid 参数显式转换到 domain 的 decimal.Decimal，金额、价格和比例不经过浮点或 JSON/map 转换桥。后续传输层必须使用 typed Proto，所有小数仍为 string。

schema 0 的历史版本只返回元数据及零 schema，参数为空，不补默认值或声称可运行。当前 schema 必须匹配实例模板、唯一具体参数形态并通过原有范围校验；未知/损坏参数、数据库故障统一返回固定 unavailable，保留内部错误链用于定位，公开文本不包含数据库细节。构造和查询不创建账号、账户、资金、配置、订单、动作或审计写入。

真实数据库、并发快照反例及工程检查见 [P07.6](validation/P07-06.md)。P07.10增加独立的[管理写事务与结果仓储](ADMIN_CONTROL.md)，包含事务内权限、版本比较、幂等记录与审计；传输/页面仍待接入，不能复用读快照充当写授权。

## 管理 gRPC

合同为 `api/strategy/v1/management.proto` 的 StrategyManagement，包含 ListStrategies、GetStrategy、ListConfigVersions。调用者必须同时通过 Deer 的 CA/mTLS 验证及方法级 `api.grpc` 身份检查；其他有效服务证书也不具备管理权限，metadata 无法伪造身份。请求携带 API 从 HttpOnly cookie 提取的 AdminSession，不接受浏览器业务 JSON 自行指定会话。无/超长原生期限、超 2KiB 请求、未知请求字段、非法页大小/身份或非规范随机会话拒绝；原生上限 5 秒，沿用 Deer 有界连接/发现及禁止透明重试规则。

响应为显式 Strategy/ConfigVersion 字段及 Maker/Grid oneof，所有小数为 string；生效配置使用消息/optional presence，未生效不会显示成版本零配置。schema 0 不返回参数；金额精度、参数联合及整毫秒约束在输出编码时也检查。错误使用固定文本和明确 Unauthenticated/PermissionDenied/InvalidArgument/NotFound/Unavailable/原生取消期限分类，不向客户端返回持久化原因。

strategy 的 `business.strategy.management_enabled=true` 显式开启，服务器 allowed_peer_services 必须包含 `api.grpc`。可与既有显式账户报价工作器同时开启；若 account_ids 为空，则必须省略 lease_ttl 和 grpc_clients，只装配查询、无交易工作器。此模式仍需 business.enabled、simulator、PG、gRPC mTLS、Nacos 注册及原有有界 poll/operation/shutdown 配置；不自动迁移、创建身份/资金/租约或授予风险。若没有开启管理功能，既有报价进程保持原配置要求且不注册管理服务。
