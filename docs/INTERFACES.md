# 接口、状态与事件约定

本文包含概念管理合同与分步落实的内部合同；P03.6 已生成 [Execution typed Proto](../api/execution/v1/execution.proto) 和 Submit/Cancel 输入边界，见 [交易合同](TRADING_CONTRACT.md)。P07.3已接入[登录/会话/注销HTTP](ADMIN_HTTP.md)，P07.7接入[StrategyManagement typed Proto](../api/strategy/v1/management.proto)的授权列表/详情/版本查询，见[查询合同](ADMIN_STRATEGIES.md)。策略管理HTTP/页面及写操作、其余管理业务API/OpenAPI尚未交付，不将概念接口标为已实现。

## 小数字段契约（强制）

- 所有接口的小数字段都定义为 string，包括请求、响应、嵌套对象、列表、WebSocket 和事件字段；金额、价格、数量、费率、比例和百分比等全部适用。
- gRPC/Proto 定义 string；HTTP/OpenAPI 定义 type: string；JSON 传输十进制字符串，例如 {"price":"123.4500","quantity":"0.001","fee_rate":"0.0005"}。禁止将这些字段声明为 float/double 或 JSON number。
- Go 边界将 string 直接解析为 decimal.Decimal，业务计算和比较统一使用 decimal，返回时直接转回 string；禁止 float32/float64 及浮点中间转换。
- 每个小数字段写明单位、范围、精度、舍入与可选值语义；服务端、调用端、生成合同与模拟器保持一致。细则见 PROTO_AND_MODEL.md，验收见 A23。

## 管理 API
api 同时交付管理前端页面与管理 API，供平台管理人员在浏览器完成 R08 的流程。由 deer.DeerApi 统一装配 Hertz 路由、认证授权、request_id、响应与错误映射，通过其复用的 Deer 获取业务 gRPC 客户端；接口边界及公共资源生命周期遵守 DEER.md。
统一前缀 /api/v1；create/update 等写请求带幂等标识；变更带 expected_version。
- GET /instruments、/market/sources、/market/health
- POST/GET /strategies；GET /strategies/{id}
- POST /strategies/{id}/config-versions；GET /strategies/{id}/config-versions
- POST /strategies/{id}/start、pause、resume、stop、cancel-orders
- GET /operations/{id} 查询异步动作；stop/cancel 未完成返回 202。
- GET /orders、/fills、/positions、/risk-events、/audit-logs
- POST /risk/kill-switch（明确 scope 和原因）；解除需要 admin 且检查恢复条件。
平仓为单独受限操作，不混入 stop；默认模拟环境实现，生产另行授权。

管理前端同源调用这些接口，保留十进制字符串；页面上的角色权限与后端校验一致。参数错误、403、409、503 及 202 异步 operation_id 均有对应页面状态。页面路由的 HTML 回退不得覆盖 /api/v1 的 JSON 错误响应；OpenAPI 与可操作管理页面分别验收。

## 行情源与实例字段

- GET /market/sources 返回统一来源标识、名称、接入状态及是否可选；首批可用目标为 binance、okx，bitget、bybit 在候选状态下不能被启用。GET /market/health 按 source+instrument 返回就绪、行情时效和异常原因。
- 策略创建与配置版本请求显式包含 string market_data_source，首批允许 binance、okx；创建结果、实例详情、配置历史返回该字段，运行状态额外返回 active_market_data_source，区分请求配置与实际生效来源。
- 参数校验覆盖来源标识、适配器启用与交易对支持；未知或未接入来源返回参数错误，不映射到其他交易所。启动时所选源不健康返回明确依赖不可用/未就绪状态，不转入可报价运行态。
- 换源使用既有配置版本接口、expected_version 和审计，按 PRD 的暂停/撤旧单/新源就绪流程生效；接口不能只更新展示字段而报价仍消费旧源。
- 来源标识及别名集中定义，使用 binance、okx、bitget、bybit；bitgit 仅记录为用户输入的 Bitget 拼写，不另建适配器或对外标准标识。行情价格、数量、费率等小数字段仍遵守 string/decimal 契约。

## gRPC 责任
StrategyService 管实例及配置；MarketService 提供快照与质量；ExecutionService 接受交易意图、撤单及恢复查询；PortfolioService 提供持仓和对账；cron 只调用同样的已授权合同。
MarketService 的行情请求及订阅显式携带 market_data_source 与 instrument，响应标明实际来源；StrategyService 传递实例已生效选择，禁止 MarketService 以全局默认值覆盖。来源字段在 RPC 中独立定义，不藏入 JSON 或无类型 payload。
交易请求包含 request_id、strategy_id、config_version、owner_epoch、account_id、instrument、deadline。网关 client_order_id 由稳定意图标识派生，重试不随意生成新 ID。

## 状态机
策略：CREATED → STARTING → RECOVERING → RUNNING → PAUSING → PAUSED；RUNNING/PAUSED → STOPPING → STOPPED。ERROR 进入受控恢复，不直接跳 RUNNING。配置生效版本和期望运行状态持久化；节点失效不篡改用户期望。
订单：INTENT → SUBMITTING → LIVE/PARTIALLY_FILLED/FILLED/REJECTED/UNKNOWN；撤单用独立 cancel 状态，避免丢失原订单部分成交信息。CANCELED 仍允许补收撤单前已发生但延迟到达的成交；累积成交单调且不超过原始量，异常隔离并对账。
UNKNOWN 非终态，不能释放风险预留。fill 事实优先用于数量累计，状态回报不能使已成交订单回退 LIVE。

## 事件
统一 envelope：event_id、event_type、schema_version、occurred_at、producer、aggregate_id、aggregate_version、request_id、payload。不同来源 seq 不可直接比较。
初始事件：StrategyStateChanged、ConfigActivated、OrderIntentAccepted、OrderStateChanged、TradeFilled、HedgeStateChanged、RiskTriggered、ReconciliationMismatch。
subject 示例：muskdeer.<env>.execution.order.v1。按账户分片的顺序需要显式设计，不能假定多消费者自然有序。消费者以 inbox 唯一键保护业务效果；event_id 去重不能代替成交自然键去重。
