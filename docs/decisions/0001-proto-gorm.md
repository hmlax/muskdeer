# ADR 0001：Proto 生成结构直接用于 GORM

状态：P01 最小生成与 PostgreSQL 验证通过；2026-10-26。适用范围：后续持久化模型，不代表业务实体和迁移已完成。

Proto 是唯一持久化结构来源，采用 protoc-gen-go 加 protoc-gen-gotag，字段通过 (tagger.tags) 声明 GORM tag。仓储直接使用生成结构及 Model，保留业务值对象与边界转换，不引入手写重复 Model 或专用 .gorm.pb.go 结构。

- Id 为 int64 自增主键；业务自然键继续以独立唯一索引实现。
- create_at、update_at 使用 UTC Unix 毫秒 int64，数据库为 NOT NULL bigint，显式 autoCreateTime:milli/autoUpdateTime:milli。业务边界用 time.Time 转换，RPC 对外时间按各合同转换为 RFC3339/Timestamp；不直接暴露数据库结构为 API DTO。
- delete_at 为 proto3 optional int64，生成 *int64，数据库 bigint 可 NULL。缺失、零毫秒及有效时间均可区分。本路线验证可空删除时间，不宣称启用 GORM 自动软删除；业务删除/默认过滤策略在 P03 明确实现与验收。
- 小数 Proto 使用 string，数据库 numeric 的位数由字段/资产决定；业务从字符串直接解析为 decimal.Decimal，计算后返回十进制字符串。P01 的 numeric(38,18) 仅为精度探针，不是全部生产资产的统一精度。

验证由 internal/persistenceprobe 使用版本化 SQL fixture 和隔离 PostgreSQL 执行，无 AutoMigrate。数据库真实验证自增、NOT NULL/NULL、联合唯一键、零值更新、numeric 往返和事务回滚。约束的显式 NULL 测试采用 Model.UpdateColumn 关闭时间回调，单独检验数据库约束；普通更新另行断言 updateAt 自动更新。

工具精确版本见 tools/versions.json 与 go.mod，具体证据见 [P01.2 验证记录](../validation/P01-02.md)。依据：[GORM 自定义时间字段](https://gorm.io/docs/models.html)、[gotag 字段选项及生成方式](https://github.com/srikrsna/protoc-gen-gotag/tree/v1.0.2)。
