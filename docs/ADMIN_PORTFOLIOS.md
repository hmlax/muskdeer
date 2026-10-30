# 管理账户持仓查询

P07.44提供[AdminPortfolios](../internal/domain/admin_portfolios.go)当前viewer只读仓储，尚未注册RPC/HTTP或页面。持仓是可重建的账户投影；核心余额与成交仍是权威，不能以管理读取补账或修改核心资产。

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

数据库与资金验证见[P07.44](validation/P07-44.md)。无新模型、迁移、依赖、权限或前端资产，不能据此声明完整R08/A27/L1完成；typed接口及管理页面继续后续步骤。
