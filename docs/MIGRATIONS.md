# 版本化数据库升级

P03.2提供显式迁移工具，服务启动及dev-up都不会自动执行迁移或AutoMigrate。当前版本30包含迁移记录、交易对/实例/配置来源身份、typed模板参数、策略/执行/行情租约、模拟核心、OMS/持仓、行情快照/基准、持久动作/运行incarnation、管理身份/审计、独立请求运行目标、管理操作收据/结果、运行配置激活预算同步、[配置发布收据/待激活最终门禁](ADMIN_PUBLICATION.md)、[策略风险控制收据/最终开关门禁](ADMIN_RISK.md)及[指定订单撤单请求/结果](ADMIN_CANCELLATIONS.md)；后续业务表继续按新版本加入。

## 使用

先准备专属数据库及 schema，按配置验证凭据和权限，再明确执行：

```sh
make build
.cache/bin/migrate -apply -config /path/to/service.json
```

本地开发可使用 `make dev-up` 生成的 `.cache/dev/execution.json`，先 source `.cache/dev/environment.sh`。工具只装配配置中的 PostgreSQL，不创建业务进程、日志文件、Redis、Nacos 或消息客户端。Go 固定 1.27.3，整个命令有 30 秒总预算及 SIGTERM 取消；数据库各语句还受配置 statement_timeout 限制。

成功输出typed JSON `{"status":"applied","version":30}`，包括已经是当前版本的情况；参数错误退出2，配置/连接/历史/执行/清理错误退出1。必须显式传-apply，不支持自动降级、删除卷或回写修复历史。错误文本不输出SQL、凭据或原始配置，历史不一致有独立安全提示。

## 结构与执行

迁移定义为 [内嵌 SQL](../internal/migrate/sql/0001_schema_migrations.sql)，按连续版本编号加载，记录文件名与精确字节 SHA-256；`.gitattributes` 固定 SQL 的 LF 行尾。仅运行随二进制内嵌的已审查 DDL，不接受用户文件路径或动态业务 SQL。

SchemaMigration 的唯一结构来源为 [Model Proto](../deer/proto/model/v1/migration.proto)，由既有 protoc/gotag 生成；记录含 Id 自增主键、create_at/update_at 非空及真实 nullable delete_at。GORM 自动填入 Unix 毫秒时间。物理表名由生成类型及统一 GORM 命名推导，数据读写始终使用 WithContext(ctx).Model(&modelv1.SchemaMigration{})。

一次事务获取项目专属 PostgreSQL advisory transaction lock，创建尚不存在的 ledger，验证全部既有版本/名称/校验和后执行所有待升级 DDL，并插入对应生成模型记录。锁随提交/回滚释放，所有读写使用同一事务和调用方 context。固定 advisory-lock 控制语句及已审查迁移 DDL 是本工具的 SQL 范围，不放行业务 Raw/Exec 字符串选表。

历史不是当前内嵌定义的完整前缀、校验和/文件名改变、版本超过当前二进制、逻辑删除记录或已有空 ledger，均拒绝升级；不静默补写或重置。失败的语句会回滚该批新增 DDL 和记录，已提交的历史保留。提交结果不确定时可以使用同一二进制重试，先依据持久记录判断剩余版本。

## 验证边界

真实隔离 PostgreSQL 验证并发 writer、重复执行、四个公共字段约束及时间、nullable 往返、未来/篡改/空历史拒绝、失败 DDL 回滚、锁竞争取消以及实际命令。见 [P03.2](validation/P03-02.md)。事务迁移不支持 PostgreSQL 要求脱离事务的操作；未来此类变更应单独设计，不隐式取消锁或原子性。生产执行继续遵守独立授权范围。

版本 2 的既有库升级、实例独立来源和恢复、请求/active 复合外键、生成模型小数及公共字段验证见 [P03.4](validation/P03-04.md)。来源字段持久化不代表已完成运行安全切换。

版本 3 的具体参数 Proto/JSONB、模板与 string 小数约束见 [参数说明](STRATEGY_PARAMETERS.md)。旧 metadata-only 配置保留 schema=0，业务解码拒绝将其作为可计算参数；新控制流程须提交完整 schema=1 版本。

版本 4 加入保留 epoch 的策略/账户执行租约，使用数据库时钟和行锁协调；范围及最终执行端约束见 [租约说明](LEASES.md)。

版本 5 加入模拟网关命令/订单模型、事务结束授权约束及有限 numeric 检查；见 [模拟器边界](SIMULATOR.md)。尚未包含资金账本及运行处理器。

版本 6 加入模拟账户、资产余额与不可变成交事实，订单保存费用率；精度与现货结算规则见 [模拟器资金说明](SIMULATOR.md#p039-现货资金与显式成交)。

版本7加入 OMS 接收、共享预算、短期风险快照和 typed 事务 outbox，实际事务范围见 [OMS 说明](OMS.md)。

版本8加入核心回报版本与OMS命令关联，目标事务重新检查风险/预算及意图身份；状态与预算恢复见 [OMS 派发说明](OMS.md#p042-派发与查询恢复)。

版本9加入独立持久撤单请求及 typed cancellation outbox；未派发原子取消与终态预留语义见 [OMS 撤单说明](OMS.md#p043-持久撤单请求)。

版本10加入持仓起点/投影、成交自然键与inbox；范围和重建规则见 [持仓说明](PORTFOLIO.md)。

版本11加入不可变来源/产品/LIVE或REPLAY/typed质量政策绑定，以及保留epoch的行情发布租约。绑定与执行产品实际base/quote资产对应，数据库拒绝静默改绑、删除和改变已绑定底层资产；最终行情发布目标的fencing继续下一版本，见 [行情说明](MARKET_DATA.md)。

版本12加入有界当前行情快照/诊断基准、durable revision及native连接/簿序列检查。最终事务提交重新验证发布owner/epoch/期限与健康时效/价差；旧发布者、同序列改价、过期引用及未接受价格改基准都拒绝。typed政策增加准确字段/类型/范围的数据库检查，NULL不能通过缺字段检查。详见 [快照说明](MARKET_DATA.md#p056-快照发布目标与基准恢复)。

版本13将风险许可绑定具体feed、来源版本、发布者epoch/最低revision及最大行情年龄；实例模式持久且不可原地修改。升级为历史实例记录REPLAY，并关闭无行情来源证据的旧许可、增加revision，不自动重开。模拟新单最终提交另有当前行情门禁，取消保留。业务许可生产仍待P06后续，详见 [风险准入](RISK_ADMISSION.md)。

版本14加入固定风险评估原因，关闭许可可记录无发布者的epoch0，开启许可仍需真实正数epoch/来源证据。风险评估器从对账、显式预算和当前所选行情产生有限期资格；周期装配及全范围逐单限额继续后续P06，见 [风险准入](RISK_ADMISSION.md)。

版本15加入报价币平台/交易对额度、单笔上限、最终模拟门禁及持久义务总额核对，保护额度范围/意图业务身份。已有未结义务带入关闭的政策行，保留预留，不推断未来额度或自动启用；部署需有序升级服务、显式配置政策。详见 [风险准入](RISK_ADMISSION.md)。

版本16统一最终global门禁与释放路径的预算→意图→全局额度锁顺序，解决跨意图提交/释放的循环等待；保留版本15校验和及金额/身份检查。见 [并发回归](validation/P06-05a.md)。

版本17加入最坏库存最终门禁、账户共享的最低已生效硬限、激活预算互斥及typed/已引用参数版本不可变；降低政策不能跨过已有义务，退役前需清理未终态订单。既有超限账户不因此获得新买入许可；见 [风险准入](RISK_ADMISSION.md)。

版本18增加生成模型StrategyAction，先持久PENDING，再以CAS/策略租约写DISPATCHING后才允许调用执行端。决策键永久绑定业务字段及稳定RPC键，尝试后的截止时间不可重写；缺失结果保持UNKNOWN，已受理未完成保留WAITING，均阻止新增提交键。只有未派发动作能丢弃。提交前独立deferred门禁重新检查真实策略/租约/时间及关联OMS身份，失权整体回滚动作状态；没有初始化资金、风险许可或自动派发。新表同样含Id自增主键、create_at/update_at非NULL、delete_at可NULL，历史保留并禁止删除/软删除；详见 [动作日志](STRATEGY_ACTIONS.md)。

版本19在StrategyInstance增加runtime_owner_id/epoch。零epoch保留历史元数据和明确模拟夹具；实际生命周期先记录正epoch恢复状态，再允许进入RUNNING。正epoch不可退回零或改写同epoch身份，提交末尾重新检查真实租约；停止/暂停完成及配置切换须无OMS、核心订单或动作义务。所有权元数据不替代最终执行端fencing，不因升级自动启动策略或开放风险，见 [生命周期](STRATEGY_LIFECYCLE.md)。

版本20增加NOT_ACCEPTED及原业务键永久门禁。在当前策略准入互斥/lease和READ COMMITTED下，只有原截止时间已过且全部受理/核心证据均不存在时才允许判定；提交末尾SQL独立重验。旧RPC/核心键不能续期复活，相关受理历史保留；未确认取消的父订单不因此释放资金。只适用于当前L1共享数据库合同，见 [截止证明](STRATEGY_ACTION_RESOLUTION.md)。

版本21增加已受理L1意图的CORE_NOT_ACCEPTED权威证明及核心身份永久门禁。原提交期限已过、等待在途核心事务结束且全部核心命令/订单历史均缺失，才能原子释放风险并记REJECTED；通用仓储不启用此能力。末尾SQL拒绝旧快照/伪造，原键不得复活或删除，见 [已受理请求恢复](SIMULATOR_SUBMISSION_RESOLUTION.md)。

版本22补齐已受理取消的无效果证明、稳定核心键约束/索引及永久门禁；仅关闭原取消请求，不修改父订单、冻结或任何风险预算。核心提交/取消均要求当前READ COMMITTED快照，拒绝首次查询也可能陈旧的准入；见 [取消无效果证明](SIMULATOR_CANCELLATION_RESOLUTION.md)。

版本23加入生成AdminUser/AdminSession/AdminAudit及真实身份/会话/审计约束：credential_version关闭旧权限，token/CSRF仅摘要持久化，会话身份/期限/撤销不可复活，审计追加不可改删。启动不自动准备用户或默认密码；见[管理认证基础](ADMIN_AUTH.md)。

版本24为StrategyInstance加入独立desired_state，只允许RUNNING/PAUSED/STOPPED且与desired_running一致。升级按原bool及PAUSING/PAUSED实际状态回填；新建旧式实例在插入触发器内规范化，旧bool-only启停请求映射RUNNING/STOPPED，显式暂停必须同时命名PAUSED。实际运行状态及租约接管不能改写该请求目标；有owner的运行状态转换末尾再核对请求目标，原恢复/终态义务/fencing约束保留。后续管理接口在自己的鉴权/幂等/审计事务内写入该目标，本字段自身不构成匿名管理入口。

版本25增加生成AdminStrategyOperation，按操作者/幂等键保存绑定审计及实例版本/来源/目标的ACCEPTED收据。SQL验证当前operator会话及审计、禁止身份/历史改删；SUCCEEDED需真实目标状态及当前运行租约或已清空义务证明，SUPERSEDED需目标已被替换。升级不改变旧实例/配置或创建用户/资金/风险许可；见[管理控制基础](ADMIN_CONTROL.md)。


版本26增加生成AdminStrategyCreation及不可变创建收据表，按操作者/幂等键唯一。SQL核对当前operator会话、不可变审计、实际CREATED/请求STOPPED的初始配置及零预留独立预算，等待后按数据库时钟检查到期，禁止改删历史；原实例/参数/资金/运行目标均不改写。创建业务由显式仓储调用，不因升级自动开通用户或交易资源，见[创建事务](ADMIN_CREATION.md)。

版本27同步安全配置激活与策略预算，必须无旧义务且满足当前参数和共享最坏库存；改变预算不自动打开已关闭的资格开关。升级保留旧已生效配置、额度、预留及 Enabled，见[配置发布](ADMIN_PUBLICATION.md)。

版本28增加生成 AdminStrategyPublication 及不可变发布收据，当前 operator 会话/审计与请求版本共同约束发布。请求配置未激活时，OMS 与最终核心拒绝旧配置继续新增风险；升级不隐式激活或切换行情，见[发布合同](ADMIN_PUBLICATION.md)。

版本29增加生成 AdminStrategyRiskChange 及不可变风险收据，风险管理版本独立于资金 Revision。关闭仅影响本策略新风险，保留义务/资金和撤单能力；恢复要求当前 admin、确认配置/目标与有期限的恢复证明，提交时再检查权限及义务。模拟核心还拒绝绕过 OMS 的已管理策略关闭后提交。旧预算与状态不重写，见[风险控制](ADMIN_RISK.md)。

版本30增加生成 AdminOrderCancellation、策略范围待处理索引及不可改写身份/结果约束。指定订单请求只保存权限/审计/确认与结果，原子关联现有取消动作；不改写已有策略目标、订单、资金或风险开关。只有 OMS 终态、该单零预留及无活动核心订单才可成功，详见[指定订单撤单](ADMIN_CANCELLATIONS.md)。
