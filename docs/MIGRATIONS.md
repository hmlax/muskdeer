# 版本化数据库升级

P03.2 提供显式迁移工具，服务启动及 dev-up 都不会自动执行迁移或 AutoMigrate。当前版本 16 包含迁移记录、交易对/实例/配置来源身份、typed 模板参数、策略/执行/行情租约、模拟核心、OMS/持仓、行情绑定及有发布授权的快照/基准；后续业务表继续按新版本加入。

## 使用

先准备专属数据库及 schema，按配置验证凭据和权限，再明确执行：

```sh
make build
.cache/bin/migrate -apply -config /path/to/service.json
```

本地开发可使用 `make dev-up` 生成的 `.cache/dev/execution.json`，先 source `.cache/dev/environment.sh`。工具只装配配置中的 PostgreSQL，不创建业务进程、日志文件、Redis、Nacos 或消息客户端。Go 固定 1.27.3，整个命令有 30 秒总预算及 SIGTERM 取消；数据库各语句还受配置 statement_timeout 限制。

成功输出 typed JSON `{"status":"applied","version":16}`，包括已经是当前版本的情况；参数错误退出 2，配置/连接/历史/执行/清理错误退出 1。必须显式传 -apply，不支持自动降级、删除卷或回写修复历史。错误文本不输出 SQL、凭据或原始配置，历史不一致有独立安全提示。

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
