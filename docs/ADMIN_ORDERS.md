# 管理订单查询

P07.32提供当前 viewer 权限下的策略订单仓储列表与详情，P07.33增加typed gRPC/HTTP入口，P07.34在策略详情增加只读页面。数据来自真实 OMS 意图与可关联的模拟核心事实，不创建、确认或撤销任何订单，不触发对账、释放预留或开放风险。独立管理撤单继续后续步骤。

## 范围与字段

[域合同](../internal/domain/admin_orders.go)以明确策略 ID 为必填范围，按 OMS 自增 ID 升序使用 exclusive cursor，每页1–100。可选择一个已定义 OMS 状态及 open_only；开放状态包括 INTENT、SUBMITTING、LIVE、PARTIALLY_FILLED、UNKNOWN，终态与 open_only 组合拒绝。详情同时绑定策略 ID 和订单 ID，不能越过同账户的兄弟策略。

每条返回业务账户/交易对/稳定客户端订单 ID、订单自己的配置版本与历史行情来源、方向、价格/数量/已确认成交、名义额预留、TIF/reduce_only、OMS 与取消状态、最后已确认核心 ID/版本、记录修订/截止/创建/更新时间及固定业务错误代码。Go金额保持 decimal.Decimal，接口小数统一 string。提交幂等键、请求追踪键、策略及执行租约身份不进入管理视图。

以账户、策略、交易对和客户端身份关联实际核心记录，独立返回最新可见核心 ID、状态/取消状态、成交数量、冻结资产/金额、费用率、修订及核心记录更新时间。核心已成交但 OMS 尚未确认时，两种状态和预留分别保留；提交结果未知但核心存在时也明确显示二者差异，不在读取中改成 LIVE 或释放额度。

尚未观察到核心记录不构成无受理证明，不将缺记录转换为拒绝或预算释放；权威无效果证明仍使用既有截止/fencing 恢复协议。当前分页主集合是 OMS 订单，包含未派发/未确认的意图；没有 OMS 的裸核心模拟场景不被伪造为新业务订单。

## 一致性和失败

[查询实现](../internal/repository/admin_orders.go)复用现有管理 read-only REPEATABLE READ：当前会话/角色/凭据/到期、策略范围、OMS、不可变配置与核心都在同一快照内读取。核心事实在认证读取后由另一个事务更新，不混入这次响应；后续新读取可见新事实。每个调用重新鉴权，既往HTTP身份或曾经成功的页面不替代仓储边界。

使用生成模型直接读取，所有 GORM 操作均绑定 WithContext/Model；批量配置和核心查询随页长有界，不做逐单 N+1 查询。无法关联已确认核心、业务字段不相符、存储失败或父取消时整页/详情失败，不返回部分 OMS 成功或游标。对外错误固定，内部诊断仍可通过私有错误链定位。

实际资金、UNKNOWN、部分成交、历史来源、分页/策略隔离、并发核心事实与失败/权限验证见[当前进度](PROGRESS.md)。此能力没有新增表或默认权限，不能计为完整 R08/A27。

## typed 传输边界

[订单Proto](../api/strategy/v1/orders.proto)的StrategyOrderQuery提供ListStrategyOrders与GetStrategyOrder。只随strategy.management_enabled注册，验证API mTLS身份、最多5秒原生截止、≤2KiB请求和当前viewer；不要求控制能力，不创建交易工作器。API复用既有strategy连接，HTTP单次最多3秒，继续继承更早父取消。

| GET 路径 | 参数与返回 |
|---|---|
| /api/v1/strategies/:strategyID/orders | ListStrategyOrdersReply；limit默认50、范围1–100，after_id默认0；可选state精确OMS枚举、open_only严格true/false |
| /api/v1/strategies/:strategyID/orders/:orderID | GetStrategyOrderReply；两个ID均为规范正int64，无query |

只接受唯一会话Cookie；拒绝GET body、超过1024字节query、重复/未知参数、非规范ID和矛盾筛选。错误使用固定400/401/403/404/503，不返回私有存储原因；未开启查询路由404，写方法405。响应no-store，所有ID/修订/毫秒和小数为JSON string。

具体StrategyOrder与可空core分别携带OMS及核心观察，confirmed_core_id/confirmed_core_revision表示OMS最后确认，core.revision可以更新。core缺失以null表达，不能变为“未受理”；UNKNOWN同时存在core合法，OMS LIVE/PENDING同时core CANCELED/CONFIRMED也合法。共享显式字段校验器检查策略与详情身份、升序游标/状态筛选、金额精度及非负边界、确认版本和来源，不强求OMS与核心状态/成交相同。下游异常成功整页拒绝，不泄露部分数据。

## 策略订单页面

策略详情显式“查看订单”后读取，不自动触发控制或风险操作。三角色都可按当前viewer权利查询，默认20条，可选择1/20/50条；按OMS状态、仅开放义务筛选，条件变化重置ID游标及详情选择。下一页使用服务端exclusive cursor，上一页使用当前页栈，刷新清除旧详情并重新读当前页。

列表分别显示历史版本/来源、价格数量、OMS状态/撤单/预留和核心状态/冻结；详情补全业务身份、TIF/reduce_only、OMS已确认成交/版本、核心独立成交/版本/费用率及UTC时间。核心缺失明确提示不能证明未受理。所有ID/版本保持string及BigInt检查，金额仅十进制文本验证/比较，不转换Number；Number仅用于整数页长和时间展示。

浏览器单次6秒等待，切换/卸载取消；加载、空页、权限/依赖/协议故障、找不到和重试分别处理，失败清空旧数据，401返回登录。无浏览器持久存储、写入或隐式对账。宽表在局部滚动容器，详情长值可折行。`make admin-orders-browser`进入CI/e2e第八组，实际验证范围见当前进度。
