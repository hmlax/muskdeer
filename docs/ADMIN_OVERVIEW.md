# 运行概览

P07.51先交付[AdminOverview只读仓储](../internal/domain/admin_overview.go)的策略数据库观测；尚无本功能RPC、HTTP或页面。服务健康另接现有Deer实际gRPC readiness和新鲜发现查询，不以Nacos在线或存储RUNNING冒充可交易证明。

## 策略数据库观测

当前viewer及以上角色在同一只读Repeatable Read事务中授权、取得数据库观测时间、聚合统计、读取有界策略页及相关租约。账户与请求来源可选精确筛选，来源指strategy_instances.market_data_source，不替代实际生效来源。页长1–100、ID升序游标；统计覆盖整个筛选范围，不受游标影响。归档策略排除，未知范围返回空结果，不表示当前账户存在。

Total、RequestedRunning及StoredStates分别记录策略数、请求运行标志计数和存储状态计数。请求停止/暂停但存储仍RUNNING的中间状态合法，不能合并成当前活跃数。统计使用固定SQL分组和有界18组结果，不把全量策略读入内存；未知/重复/乱序分组、非正计数或int64溢出整页拒绝。无策略时也有真实数据库观测时间、空页与零统计，不创建资源。

每页保留原AdminStrategy元数据与可空AdminLeaseObservation：

- nil：没有活动的持久租约槽，不虚构epoch0。
- 已有epoch0槽：合法未领取记录，OwnerRecorded=false，期限0，不是当前owner。
- OwnerRecorded：仅表示保留了非空持有者记录；释放保留owner/epoch、仅清零期限，因此释放后仍可为true。旧格式持有者按数据库合法记录保留，不以当前新owner认证格式误删历史。
- UnexpiredAtObservation：记录持有者且期限晚于这次数据库观测时间；不承诺读完之后仍有效。
- RuntimeMatchesLease：存储运行owner/epoch与保留租约值相等；过期或释放时可能仍相等，与期限观测分开。

采样的匹配租约不证明进程存活、依赖健康、行情新鲜或持仓已对账，不授权交易。接管后的新lease不改写旧runtime观察；读取不Acquire/Renew/Release、不推进生命周期、不刷新风险或创建订单/资金。实际下单仍逐次执行全部预算、行情、风险与最终fencing检查。

## 边界与后续

数据库故障、非法数据、取消或权限失效不返回部分计数、时间或页；公开错误固定，私有原因只保留内部错误链。观察不做网络探测、读取配置秘密或动态服务装配；公共资源继续由每进程唯一Deer提供。源码/真实PG/race证据及未运行范围见[P07.51](validation/P07-51.md)。

下一步接typed查询、真实服务观测和概览页面；P07及完整R08/A27/L1尚未完成，P08–P10继续，P11真实合同与测试环境仍待资料。
