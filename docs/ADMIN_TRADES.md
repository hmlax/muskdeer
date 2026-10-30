# 管理成交查询

P07.41提供[AdminTrades](../internal/domain/admin_trades.go)只读仓储，P07.42增加具体typed gRPC/HTTP；管理页面继续后续步骤。数据主集合为模拟核心的真实成交，不能用持仓投影是否收到回报决定成交是否存在。

## 范围与身份

每次调用在当前viewer授权的read-only REPEATABLE READ事务内读取。列表必须明确策略ID，使用核心成交自增ID升序exclusive cursor，每页1–100；可按精确BUY/SELL及原OMS订单ID筛选。详情同时绑定策略ID和核心成交ID，账户和交易对也由策略限定。同账户兄弟策略的订单筛选及成交详情返回不存在。

订单关联仅采用核心订单的initial_command_id所指首次SUBMIT命令中持久化的intent_id，再验证原意图的策略、账户、交易对、配置版本、客户端身份和交易条件。原始命令没有intent_id时返回可空订单身份；即使存在同client_order_id的OMS意图，也不补认、不确认、不释放额度。按该OMS意图筛选时不返回原本未关联的成交。未提交订单或空结果不构成网关无受理证明。

## 字段与精度

成交返回核心成交ID、模拟执行场所、交易流水ID、核心订单及客户端ID、可空原OMS订单ID、策略/账户/交易对、订单自己的配置版本和历史行情源、方向、价格、数量、名义额、费用与费用资产。RecordedAt是数据库记录时间，不冒充交易所成交时间。租约owner/epoch、请求键、凭据和对手方主体不公开。

Go金额使用decimal.Decimal，后续传输必须使用十进制string。读取先约束数值范围，再验证价格改善方向、成交量不超过订单及已成交量、价格乘数量等于名义额、按订单快照费用率逐笔截断36位等于费用、费用资产为交易对报价资产。此费用规则属于本项目模拟器，不代替真实场所合同。

可空PortfolioFact仅含精确同一成交事实的收录ID和记录时间。存在表示该事实已收录，不表示持仓投影已完整、完成对账或允许新增风险：例如先收到后序卖出时，卖出事实已落库，但账户仍为SOURCE_INCOMPLETE。缺失保持nil，不从查询触发投递或补账。

## 一致性与副作用

[实现](../internal/repository/admin_trades.go)与[批量关联](../internal/repository/admin_trade_rows.go)直接使用生成Model和WithContext。核心订单、首次提交、明确OMS意图、历史配置、交易对和收录事实在同一事务快照内有界批量查询。当前读取之后另一事务收录成交，不混入本次响应；下次读取才可见。

关联字段损坏、缺失依赖、存储错误或父取消均拒绝整页/详情，不返回部分数据或游标；公开错误固定，内部保留诊断链。数据库拒绝快照中的实际UPDATE。查询不修改核心账本、OMS、持仓、inbox/outbox、额度、风险或审计；未确认OMS即使核心已全部成交仍保持原预留。

真实PostgreSQL/资金/精度/并发/权限证据见[P07.41](validation/P07-41.md)。无新增持久模型或迁移，不能据此声明完整R08/A27或L1完成。

## typed传输边界

[成交Proto](../api/strategy/v1/trades.proto)的StrategyTradeQuery提供ListStrategyTrades和GetStrategyTrade，只有strategy.management_enabled开启才注册，继续验证API-peer mTLS、当前viewer会话、≤5秒原生截止及≤2KiB请求。API复用原有唯一strategy连接及strategy_queries_enabled开关，单次3秒并继承更早父取消；不要求控制权限，不创建新交易任务或客户端资源。

| GET路径 | 输入与返回 |
|---|---|
| /api/v1/strategies/:strategyID/trades | ListStrategyTradesReply；limit默认50、1–100；after_id默认0；可选规范正int64 order_id及精确BUY/SELL side |
| /api/v1/strategies/:strategyID/trades/:tradeID | GetStrategyTradeReply；核心成交ID，详情不接受query |

只接受唯一会话Cookie、规范正策略/成交ID和至多1024字节query；GET body、重复/未知键、空筛选、非规范数字及写方法拒绝，未开启查询路由404。错误固定400/401/403/404/503，响应no-store，不泄露存储原因或部分数据。

所有身份、游标、版本、毫秒和小数均为JSON string。原订单ID采用proto3 optional int64：原始关联不存在时ProtoJSON省略order_id，不伪造0；缺收录message为portfolio_fact:null。收录仅有精确事实ID及记录时间，不带“已对账”或风险许可。具体StrategyTrade每项验证身份/历史来源/方向、十进制范围、价格乘数量、费用非负且小于名义额、可空关联及收录时间；列表同时绑定原策略、订单/方向过滤、升序exclusive cursor和页长。未知Proto字段或下游错误成功整页/详情503。

跨进程资金与只读验证见[P07.42](validation/P07-42.md)。当前只提供模拟成交场所；后续真实交易适配须按实际场所的成交身份、时间及费用合同独立验收。
