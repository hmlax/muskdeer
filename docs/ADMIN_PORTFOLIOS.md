# 管理账户持仓查询

P07.44提供[AdminPortfolios](../internal/domain/admin_portfolios.go)当前viewer只读仓储，P07.45增加具体typed gRPC/HTTP；管理页面继续后续步骤。持仓是可重建的账户投影；核心余额与成交仍是权威，不能以管理读取补账或修改核心资产。

## 范围与读取字段

列表主集合是已经初始化的PortfolioAccount，每个MVP账户绑定一个交易对。使用持仓记录自增ID升序exclusive cursor，每页1–100；可选择精确账户业务ID和交易对ID。两个过滤条件同时生效，未知账户/交易对404，已存在但尚无投影的选择返回空列表；详情按持仓记录ID读取，不制造零持仓。共享账户只有一份持仓，包含该账户所有策略，不伪造策略独占份额。

每条返回持仓ID、模拟执行场所/交易对符号、账户启用状态与版本、存储的PortfolioSnapshot、核心成交和收录统计、可空基础/报价资产核心余额、投影创建与更新时间。投影包含明确期初数量/现金/成本、当前存储数量/现金/成本/已实现损益/费用、最后已处理源ID、存储修订和生产者对账诊断。租约身份、请求键、凭据及自营主体不进入管理DTO。

三个观测彼此独立：

- 存储的Projection.Reconciled/ErrorCode来自最后一次生产者处理，可能早于当前核心事实。读取保留原值，不能把它解释为当前允许新增风险。
- Source/Collected分别统计该账户和交易对的权威核心成交及已收录事实数量、最大源ID。全局源ID可跳号，数量不是序列；两个数量或末尾ID相等也不单独证明完整对账。
- CoreBaseBalance/CoreQuoteBalance分别给出实际资产、可用额、冻结额、修订与记录时间。缺失保持nil，不补造零余额；不会从投影值反写核心。

账户可用/冻结、数量/成本/现金/费用保持非负decimal.Decimal，已实现损益允许负数。存储范围为numeric(76,36)，先检查数值精度再进行任何运算。没有引入行情估值、未实现损益或新的风险许可；后续接口小数必须使用string，前端不得经Number或parseFloat转换。

## 一致性与失败

[仓储](../internal/repository/admin_portfolios.go)、[批量关联](../internal/repository/admin_portfolio_rows.go)和[统计](../internal/repository/admin_portfolio_stats.go)直接使用生成Model、WithContext和当前viewer的read-only REPEATABLE READ。策略、执行与持仓的生产逻辑不参与读取；不调用具有写入效果的Portfolio.Get/Rebuild/Apply，不加账户/资产行锁，不新增资源或工作器。

按页批量读取账户/交易对/余额；核心成交统计使用明确Model和固定JOIN，收录统计使用其生成Model，都只接受页面中的精确账户/交易对组合，返回有界分组而非全部成交历史。所有账户策略共享统计，不按当前策略或行情来源截断历史。

认证后另一个事务真实成交，不混入当前投影/源统计/余额快照；下一次读取才看见新事实。关联缺失或字段损坏、数据库错误、父取消均整页/详情失败，不能返回存储投影的部分成功或游标；公开错误固定，内部保留私有错误链。实际UPDATE在只读事务内被PostgreSQL以25006拒绝。

数据库与资金验证见[P07.44](validation/P07-44.md)。无新模型、迁移、依赖、权限或前端资产，不能据此声明完整R08/A27/L1完成。

## typed传输边界

[账户持仓Proto](../api/strategy/v1/portfolios.proto)的StrategyPortfolioQuery在管理strategy节点的management_enabled开关下提供ListPortfolios和GetPortfolio。该服务是管理读模型，不启动portfolio消费者或调用其写入式查询。继续使用API-peer mTLS、当前viewer、最多5秒原生期限、≤2KiB具体请求；控制开关不影响读取，只有查询节点不会注册策略写入。

API沿原唯一strategy连接与strategy_queries_enabled开关提供只读路径，单次3秒并继承更早取消：

| GET路径 | 输入与返回 |
|---|---|
| /api/v1/portfolios | ListPortfoliosReply；limit默认50、1–100，after_id默认0；可选非空规范account_id及正int64 instrument_id |
| /api/v1/portfolios/:portfolioID | GetPortfolioReply；规范正int64持仓记录ID，详情不接受query |

拒绝GET body、超过1024字节query、重复/未知键、非规范ID、空筛选及重复会话Cookie。未开启查询路由404，写方法405；错误固定400/401/403/404/503，no-store，不泄漏私有存储原因或部分结果。

AccountPortfolio以具体字段携带账户/交易对/资产/记录时间/版本、必需projection和source/collected、可空core_base_balance/core_quote_balance。projection.stored_reconciled明确是存储生产者诊断；它与当前核心金额、源数量不同步合法，不在传输转换中修平。两缺失余额JSON为null。全部金额/损益和身份/游标/版本/计数/毫秒均为string，bool保持bool。

共享字段校验器先约束domain小数再String，负数仅用于已实现损益；检查完整业务身份、已定义诊断、期初零库存/零成本关系、非负计数及空计数/空末尾源ID、资产匹配的可空余额、正版本/记录时间及嵌套未知字段。列表绑定原账户/交易对/页长/升序游标，详情绑定原记录ID；错误成功整页/详情503。不同节点记录时间不作因果排序，统计和stored_reconciled均不转换为风险授权。

实际跨进程账户/资金/权限证据见[P07.45](validation/P07-45.md)，后续管理页面单独验收。
