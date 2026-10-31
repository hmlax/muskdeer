# 运行概览

P07.51交付[AdminOverview只读仓储](../internal/domain/admin_overview.go)，P07.52接通[typed gRPC](../api/strategy/v1/overview.proto)与管理HTTP读取；P07.53补充[实际服务观测读取器](../internal/serviceoverview/observer.go)，P07.54接通[typed服务HTTP消息](../api/admin/v1/services.proto)，概览页面继续后续。服务观测使用现有Deer实际readiness和新鲜发现查询，不以Nacos在线或存储RUNNING冒充可交易证明。

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

## typed查询

StrategyOverviewQuery.GetStrategyOverview仅在strategy的management_enabled下注册，可运行于无账户worker的管理查询节点；当前viewer在仓储事务重新授权。复用API-peer mTLS、最长5秒、2KiB和未知字段/嵌套会话校验，不注册新的写方法。

api在strategy_queries_enabled下借用既有strategy连接提供GET `/api/v1/overview/strategies`，三秒超时、Cookie会话、no-store。可选`account_id`及`requested_market_data_source=binance|okx`精确筛选，`after_id`为非负规范十进制、`limit`为1–100（默认50）；明确请求来源，`market_data_source`等未知参数拒绝。重复、空参数、非规范ID、GET体和非法来源400，写方法405，禁用404，失效会话401，权限拒绝403，依赖/异常成功503。

返回`observed_at_ms`、`total`、`requested_running`、`stored_states`、`items`与`next_after_id`，所有int64按ProtoJSON为string。每项包含已有Strategy元数据与可空`lease`；未持久化租约为JSON null，实际epoch0槽为非空对象，释放槽保留正代次/owner_recorded及期限"0"。不输出新造的健康/可交易布尔值。

gRPC映射与HTTP两端校验统计和、正的已知状态分组及升序唯一性/溢出、每页状态/请求目标的计数下界、筛选绑定/ID游标和可空生效配置、租约零槽/期限/匹配代次。第一页宽度和后续游标须与全范围统计一致；后页可为空且total仍大于零。精确毫秒转换前拒绝亚毫秒时间，非法/未知成功整页拒绝，不泄露部分计数。原生进程、精度和边界证据见[P07.52](validation/P07-52.md)。

## 服务观测读取器

AdminServices.Observe按api/market/strategy/execution/portfolio/cron固定角色读取，不接受任意主机或RPC目标。每页1–4个实例，按实例ID升序游标；总数独立于游标，不把当前页探测结果外推为全部服务健康。读取前后都调用当前持久会话Identity，校验viewer及同一user/session；撤销、角色丢失、会话替换或父取消整次丢弃，不返回部分成员和时间。

api来源LOCAL_API仅代表处理请求的这个API，total=1，不枚举全部API副本。借用其唯一Deer.CheckReady，包含实际监听、工作器及已配置依赖检查；不使用仅表示生命周期的Ready。其他角色来源NACOS_HEALTHY_GRPC，借用既有发现客户端按当前namespace/group/cluster读取健康、启用的gRPC注册；total不包含注册系统排除的节点，也不是部署的进程总数。

仅对既有RPC allowlist明确允许的角色，通过既有factory及指定InstanceID调用标准gRPC health Check；每次RPC由factory再次新鲜解析所需实例、执行现有mTLS服务身份校验、关闭该次自有socket，实例消失不回退其他节点。不自动增加allowlist、替换证书或构造新连接池。未配置的探测为PROBE_NOT_CONFIGURED且无ProbedAt，仍保留发现元数据。

READY/NOT_READY/UNKNOWN分别来自实际readiness结果，失败、子期限、权限拒绝及RPC容量限制分别为PROBE_FAILED/PROBE_TIMEOUT/PROBE_DENIED/PROBE_CAPACITY；未知协议字段/状态不标就绪，原始provider错误/地址不公开。本API readiness的依赖失败为NOT_READY，子期限仍单独标示。READY只表示该次readiness检查，不能替代行情、对账、风险或最终下单fencing。

整个观测最长3秒，前后身份各400毫秒、发现400毫秒、每次探测350毫秒；逐项顺序检查且最多4项，无goroutine、后台轮询、订阅、缓存或保留请求上下文。全局资源容量/清理仍归Deer及原factory。构造只校验/快照元数据与allowlist，不联网、迁移或启动工作。

MembershipObservedAt和各ProbedAt来自API本地时钟、独立采样到毫秒；成员读取与后续RPC不是原子集群快照，时钟也不作租约依据。DeclaredVersion只是注册/本地配置声明，不是RPC认证的构建版本；mTLS验证服务身份，不证明某实例名对应的进程incarnation。原记录在探测期间可能改变，不将注册ID与策略runtime owner前缀关联为当前所有权。

实现、原生进程与检查范围见[P07.53](validation/P07-53.md)。

## 服务观测HTTP

P07.54在既有strategy_queries_enabled下提供GET `/api/v1/overview/services`，借用同一AdminAuth、Deer发现客户端、RPC factory及CheckReady；没有新RPC业务服务、配置开关、池、worker或权限。远程就绪仍为标准typed gRPC health Check，不以API直接访问业务表代替业务RPC。

可选`role=api|market|strategy|execution|portfolio|cron`（默认strategy）、`limit=1..4`（默认4）及`after_instance_id`。游标为最多128字节的规范UTF-8显示身份，按实例ID比较，不当作探测地址；实际远程ID仍按既有命名目标字母表验证。未知/空/重复参数、非规范页长、非法角色/游标、GET体400，写方法405，查询禁用404。Cookie仅用于读取器当前身份校验，401/403/503返回固定错误，不附部分成员/时间或私有地址。

ServiceOverview按ProtoJSON提供role、membership_source、membership_observed_at_ms、total、probe_configured、items及next_after_instance_id；int64全为string。每项含instance_id、declared_version、status及可选probed_at_ms。未配置探测按标准ProtoJSON省略时间字段，既不为null也不为"0"；失败探测仍是有效200诊断项，不标整体服务健康。

typed映射拒绝角色/来源/本地total与权限不一致、非规范实际远程ID、非法状态/版本、探测配置/状态/时间错配、亚毫秒/零时间及分页/排序错配。当前API只允许实际CheckReady可产生的就绪/未就绪/子期限状态，不制造网络权限结果；游标耗尽仍保留全成员数。证据及未运行范围见[P07.54](validation/P07-54.md)。

下一步接概览页面；P07及完整R08/A27/L1尚未完成，P08–P10继续，P11真实合同与测试环境仍待资料。
