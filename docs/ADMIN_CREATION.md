# 受审计的策略创建事务

P07.17提供`domain.AdminStrategyCreator`和`repository.NewAdminStrategyCreator`，P07.18接入[typed创建gRPC](../api/strategy/v1/creation.proto)及实际同源HTTP，创建表单随后接入。已有[运行控制](ADMIN_CONTROL.md)及[授权目录](ADMIN_STRATEGIES.md)保持独立；本步不代表完整A27。

请求明确包含操作者范围幂等键、唯一instance_key、已开通账户、执行instrument、code_version、Binance/OKX单一来源、LIVE/REPLAY模式、具体Maker/Grid参数及原因。身份字段有界ASCII，键16–64字符，原因trim后1–256 UTF-8字节；参数先检查十进制边界/指数再做联合与范围运算，杜绝巨大指数放大。HedgeEnabled在P08实现前拒绝，不保存一个看似开启但无执行器的对冲配置。

每次事务首先重新检查当前operator/admin、独立CSRF、用户启用/credential/会话到期；按用户行锁串行化该操作者的创建键。原键且规范内容相同返回原收据，内容改变或instance_key冲突明确conflict。回放仍须当前有效权限，但不重新创建资源；后续启停或配置不会改写初始创建收据。

依赖按instrument→account顺序持有共享锁，再读账户预算、已对账且绑定同一交易对/资产的持仓锚点和指定模式的实际行情绑定。要求执行场所simulator、TRADING交易对、enabled账户/账户预算、正确报价资产、每笔金额不低于当前最小金额、真实Binance/OKX外部符号。不存在/禁用/未对账/缺绑定不回退其他来源或模式。绑定可存在而当前行情不健康；保存初始配置不代表启动或授予风险，实际启动继续走所选源健康/持仓最坏敞口/租约/最终执行门禁。

同一事务创建：

- 初始实例：实际CREATED，请求STOPPED、desired_running=false、revision/config_version=1、无active配置和runtime owner。
- 不可变typed初始配置：schema1、确切模板/参数/来源及当前操作者。
- 独立策略预算：Maker两侧side_budget之和，Grid total_budget；预留零。账户/平台总预算仍独立约束，不把该额度当作已冻结资金。
- 不可变审计及Proto生成AdminStrategyCreation收据。按actor+key唯一，绑定实际新实例/审计/初始配置；指纹来自有长度边界的标量及确定性typed Proto，仅用于hash，不存业务bytes/JSON包装。

迁移26的最终SQL校验当前operator会话、匹配审计、初始实例/参数/零预留预算与来源证明，在依赖等待后重新比较数据库时钟到期；禁止收据改写、软删除或物理删除。创建、参数、预算、审计、收据任一步失败或到期，全部回滚。账户、余额、持仓锚点、行情、lease、核心订单均不由该事务创建或修改。调用者需有原生期限，构造函数无I/O或工作器。

## 创建传输

独立StrategyCreation.CreateStrategy复用business.strategy.control_enabled显式开关；查询模式不注册该服务。API-peer-only、当前operator及独立CSRF、规范键、原生≤5秒/≤2KiB、未知根/输入/具体参数字段均验证，具体CreateStrategyInput及Maker/Grid参数共同更新，不使用bytes/JSON业务包装。公共typed映射在internal/strategywire，两个入口都在存储前验证，业务事务仍重新鉴权。

API的strategy_controls_enabled显式注册POST /api/v1/strategies，使用固定Origin、唯一会话Cookie、X-CSRF-Token、Idempotency-Key。JSON体是具体CreateStrategyInput，禁止在体内提交会话；instrument_id必须规范十进制字符串，小数必须字符串。严格ProtoJSON检查未知/重复/nested字段及类型，额外typed身份视图验证JSON string种类，体≤8KiB、无query，真实RPC最多3秒并继承HTTP取消。缺凭据401/403，参数400、内容冲突409、依赖故障503，均固定错误/no-store。

新建或原键成功回放均201及Location /api/v1/strategies/:id，返回具体CreationReceipt；初始revision/config_version=1属于创建事实，不是当前运行状态或操作结果页ID。创建不会请求运行，后续启动必须独立控制；查询当前状态使用策略详情。未确认传输结果应保留原键和原内容重试。

持久化验证见[P07.17](validation/P07-17.md)，传输与实际独立进程验证见[P07.18](validation/P07-18.md)。后续创建表单、配置版本发布及换源流程继续P07，P08–P10及完整L1仍待验收。
