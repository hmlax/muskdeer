# Proto、GORM 与迁移规范

## 结构唯一来源
config: deer/proto/config/v1；数据库结构: deer/proto/model/v1；公共枚举/错误: deer/proto/common/v1；服务合同: api/<service>/v1。生成目录由工具配置固定，严禁手改。
用户要求使用 GORM tag 自动生成 model，不再手工定义 model。默认优先验证“Proto 生成的结构直接带 GORM tag 并作为持久化类型”。若必须使用专用 .gorm.pb.go ORM 生成类型才能满足类型要求，提交技术验证结果和取舍，请用户决定，不能静默引入第二套结构。

## 每张表的必备字段（强制）

所有持久化表及其 Proto 生成 Model 均须包含以下四个字段，关联表、租约、Inbox/Outbox、审计和任务记录也不例外。文档中的用户字段名与实现映射统一如下：

| 用户字段名 | Proto 字段 / 数据库列 | 生成 Go 字段 | 数据库约束与语义 |
|---|---|---|---|
| Id | id | Id | 整数自增主键，PRIMARY KEY、NOT NULL；由数据库分配 |
| createAt | create_at | CreateAt | 创建时间，NOT NULL；插入时设置 |
| updateAt | update_at | UpdateAt | 更新时间，NOT NULL；插入和更新时设置 |
| deleteAt | delete_at | DeleteAt | 删除时间，允许 SQL NULL；未删除时为 NULL |

- 主键与自增语义从 Proto 生成 GORM tag，显式声明 primaryKey、autoIncrement；迁移同步落实约束。业务请求键、成交自然键和联合唯一键保留为独立唯一索引，不替代 Id 自增主键。
- 时间统一 UTC。createAt/updateAt 的 NOT NULL 必须落实到数据库；只在应用层校验不算完成。CreateAt/UpdateAt 使用显式 autoCreateTime/autoUpdateTime 配置或经验证的等效生成机制，不能依赖 CreatedAt/UpdatedAt 默认命名约定。
- deleteAt 必须支持真实 SQL NULL 与时间值往返，禁止用零时间、空字符串或 0 代替 NULL。如采用软删除，须验证删除和默认查询过滤行为，不能仅凭字段名或普通 timestamp tag 声称已支持。
- 四个字段均由 Proto 与生成配置统一定义，不手写公共 Model、嵌入手写 gorm.Model 或修改生成文件规避约束。自定义时间类型及 nullable 转换纳入 P01 验证。

GORM 自定义时间字段配置依据：[官方模型文档](https://gorm.io/docs/models.html)。

## 数据访问必须绑定 Model（强制）

- 所有 GORM 查询、统计、新增、更新、删除与批量操作显式使用 db.WithContext(ctx).Model(&model.X{})；事务中使用 tx.WithContext(ctx).Model(&model.X{})。model 表示项目生成的模型包，X 为对应实体。
- 禁止 db.Table("xxx")、tx.Table("xxx") 以及任意别名、封装函数或链式调用中的 .Table(...)，不得通过传表名的公共函数绕过。规则覆盖业务服务、deer 公共组件、脚本、测试和测试数据准备。
- 表名由生成模型及统一命名配置确定，不在调用处选择字符串表名；联表和子查询也必须以对应 Model 构造。
- 禁止用 Raw/Exec 拼接业务表名规避本规则；版本化迁移可使用明确的 DDL SQL，但不因此放行业务层字符串选表。
- 查询示意：db.WithContext(ctx).Model(&model.StrategyInstance{}).Where("id = ?", id).First(&instance)。示意仅说明模型绑定方式，不代表代码已实现。

Model 查询方式参考：[GORM 官方查询文档](https://gorm.io/docs/query.html)。

## P01 技术验证必须先于大规模业务生成
P01 已验证的直接生成路线与时间/小数表示见 [ADR 0001](decisions/0001-proto-gorm.md)。公共持久化时间使用 UTC Unix 毫秒：create_at/update_at 为 int64、NOT NULL bigint，delete_at 为 optional int64、nullable bigint；对外时间 DTO 与业务 time.Time 在边界转换。完整业务模型及软删除语义由 P03 继续实现。
Go 固定为 1.27.3。固定 protoc、protoc-gen-go、protoc-gen-go-grpc、Hertz hz、tag 注入/ORM 插件的精确版本及来源，并在 Go 1.27.3 下验证 GORM v2、PostgreSQL 驱动及生成工具兼容性。区分 protobuf tag 注入工具与 protoc-gen-gorm 专用类型生成，不混用命令或猜测 option 名。
用最小 Proto 在真实 PostgreSQL 测试：Id 自增主键、createAt/updateAt 的 NOT NULL 与自动赋值/更新、deleteAt 的 NULL/时间往返、联合唯一索引、nullable、0/false 更新、timestamp、numeric(精度,小数位)、反序列化、查询扫描、重复生成。所有数据操作使用 Model；确认 GORM 忽略 protobuf 内部字段。软删除如启用，额外验证删除及过滤行为。
string 的 gorm:type:numeric 标签本身不提供精确运算能力；生成字段需有验证过的转换/Scanner/Valuer 或仓储边界转换，业务计算用明确 decimal 值对象。

## 类型规则

### 小数统一使用 string / decimal（强制）

- 所有接口中的小数字段统一定义为 string，适用于 HTTP/OpenAPI、gRPC/Proto、WebSocket、消息事件、请求、响应、嵌套字段和列表元素；价格、数量、金额、余额、损益、费率、比例、百分比及其他小数参数全部纳入。字段承载的某个值恰好为整数时，仍按该字段的小数 string 契约传输。
- Proto 小数字段使用 string，禁止 float/double；OpenAPI 使用 type: string，禁止 number/float/double；JSON 使用带引号的十进制字符串，如 "123.4500"。配置和模拟器中的相应小数输入遵守相同规则。
- Go 中小数值的存储、计算、比较、累加与测试期望统一使用 decimal.Decimal；禁止 float32/float64、浮点字面量初始化以及先浮点后 decimal 的转换。接口 DTO 和持久化 Proto 保持 string 契约，在边界显式转换为业务 decimal，不手写重复 ORM Model。
- 输入从十进制字符串直接解析为 decimal，计算完成后直接格式化为十进制字符串；全链路不得经浮点数中转，包括 JSON 解码、配置读取、适配器、数据库扫描与测试数据准备。
- 验证十进制格式、有效位、小数位、范围及正负限制，拒绝 NaN/Inf/科学记数法等未约定输入；可选字段的缺失、NULL、空字符串与 "0" 明确区分，非法字符串不得静默当作零。
- 除法、量化与输出必须明确精度和舍入模式；每个字段声明单位、允许的小数位及格式化规则，不能靠浮点格式化或无说明的截断完成金额换算。
- 数据库小数使用 PostgreSQL numeric/decimal，禁止 real/double precision。生成模型的 string/numeric 与业务 decimal 转换须验证精度及 NULL 语义；类型约束不能仅靠 gorm:type:numeric 标签宣称完成。
- P01 统一选择并锁定项目 decimal 库及精确版本，验证字符串解析、计算、格式化和数据库往返；此准备包尚无 Go 依赖，不预先宣称已集成。

### 其他类型规则

- 数据库 precision/scale 按资产规则确定，不一刀切猜测。买价向下取 tick、卖价向上取 tick；数量向下取 lot，重新检查最小金额。
- 时间统一 UTC 保存，对外 RFC3339 或标准 protobuf Timestamp；nullable 与零时间区分。
- Proto enum 零值 UNSPECIFIED；字段删除 reserve 编号和名称，禁止复用。
- RPC DTO 与数据库 Proto 分开；转换属于边界代码，不是手写第二份数据库 Model。
- 算法值对象、接口和内存状态允许普通 Go 类型；禁止以“无额外 Model”为由把全部业务绑到 ORM。

## 初始持久化实体
Instrument、StrategyTemplate、StrategyInstance、StrategyConfigVersion、StrategyLease、ExecutionShardLease、CapitalAllocation、RiskReservation、OrderIntent、OrderProjection、TradeFill、HedgeTask、PositionProjection、RiskEvent、OutboxEvent、InboxEvent、AuditLog、CronRun。
本所唯一成交键至少包含 venue/account/instrument/trade_id，具体按实际接口确认。OrderIntent 使用唯一业务请求键；配置版本唯一(instance_id,version)。外键、索引、访问所有权、保留期写入迁移说明。
StrategyInstance/StrategyConfigVersion 的模型与接口 Proto 显式定义 market_data_source，保存实例当前生效选择及各配置版本的来源；候选值及接入状态分开校验。运行状态区分请求配置与 active_market_data_source，重启/接管从持久化版本恢复。来源标识集中定义，首批 binance/okx，候选 bitget/bybit；不能仅在进程内保存选择或修改生成文件添加字段。

## 生成与迁移
make generate：Proto 校验 → Go/gRPC/Hertz 代码 → 经验证的 tag/ORM 生成 → gofmt。make generate-check 在干净临时目录重生成比较，禁止覆盖用户未提交文件后声称无差异。
数据库迁移按版本执行，有校验和、执行记录及锁；不由所有服务启动时并发迁移。结构性破坏变更单独审查，生成器不会代替迁移设计。仓库必须包含从空库升级及已有库兼容测试。
