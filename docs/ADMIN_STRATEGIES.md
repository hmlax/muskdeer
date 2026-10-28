# 策略管理查询

P07.6 提供 `domain.AdminStrategies` 和借用现有数据库的 `repository.NewAdminStrategies`。尚未注册管理 gRPC、HTTP 或策略页面，不能据此将 R08/A27 标为完成。身份入口见 [管理 HTTP](ADMIN_HTTP.md)，运行目标与实际状态见 [生命周期](STRATEGY_LIFECYCLE.md)。

每次 List、Detail、Versions 均在同一个只读 REPEATABLE READ 事务内校验持久会话、用户启用状态、credential_version、数据库时钟到期及 viewer 权限，然后读取业务数据。注销或凭据变更后的新查询不能沿用此前 API 身份检查的结果。查询是快照观测，不是新增交易风险的授权；并发注销不撤回已经开始的只读快照。

List 按实例自增 ID 升序，Versions 限定一个实例并按配置行自增 ID 升序。`after_id` 为排他游标，limit 必须为 1–100；多读一行判断下一页，末页游标为零。实例及配置均排除软删除。分页请求各有快照，不承诺跨多次请求的固定全集。不存在的实例返回明确 not-found，不伪装成空版本列表。

Detail 在同一快照内返回实例、完整请求配置和可为空的实际生效配置。请求来源、实际来源分别来自各自持久身份，不将 OKX 请求显示成已生效，不从状态名推导请求目标。版本中的 Maker/Grid 参数显式转换到 domain 的 decimal.Decimal，金额、价格和比例不经过浮点或 JSON/map 转换桥。后续传输层必须使用 typed Proto，所有小数仍为 string。

schema 0 的历史版本只返回元数据及零 schema，参数为空，不补默认值或声称可运行。当前 schema 必须匹配实例模板、唯一具体参数形态并通过原有范围校验；未知/损坏参数、数据库故障统一返回固定 unavailable，保留内部错误链用于定位，公开文本不包含数据库细节。构造和查询不创建账号、账户、资金、配置、订单、动作或审计写入。

真实数据库、并发快照反例及工程检查见 [P07.6](validation/P07-06.md)。后续先接入经过 API 服务 mTLS 身份验证的管理 gRPC，再接 Hertz/React 查询页面；管理写入仍需事务内权限、版本比较、幂等操作记录与审计，不能复用读快照充当写授权。
