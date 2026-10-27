# 可执行工程规范

`make contract-check` 用固定 protoc 编译实际描述符，并以固定 Go 工具链检查项目自有 Go/Proto。它同时作为 generate、generate-check 和 verify 的前置步骤；不运行或修改业务数据。

## 检查范围

- 自有 Proto 中的 float/double 字段，包括嵌套及 repeated 元素，拒绝生成；小数边界保持 string。
- model Proto 的 id/create_at/update_at/delete_at 必须为约定 int64；检查生成的 GORM column/bigint、主键自增、NOT NULL、自动毫秒时间及可空删除时间。numeric/decimal 持久化字段必须为 string。
- 手写 Go 的浮点字面量、直接/带括号的 Table 调用和手写 TableName 覆盖被拒绝；字符串和模型元数据字段读取不会被误认为调用。生成代码由生成一致性与 Proto 描述符检查负责。
- `make lint` 启用固定 golangci-lint 中已有的 forbidigo 类型分析，识别 import/类型/DB 变量别名及 GORM Table 方法值，禁止 Table、启动 AutoMigrate、float32/float64、decimal.NewFromFloat 系列和 strconv 浮点转换。
- GORM Raw/Exec 仅允许 internal/migrate 中已审查的 DDL/固定锁控制范围，该例外不会放开 Table/AutoMigrate 或浮点。业务数据访问仍须显式 WithContext/生成 Model，通过实际仓储测试及审查核验。

这些是结构/常见语义写法门禁，不证明不存在反射、动态调用或间接跨包绕过；业务校验、运行约束及 SQL 行为仍需测试和审查，不能用一次 policy passed 代替完整 A01/A22/A23。

## 门禁自身验证

`make policy-test` 在 .cache 中创建不会执行 SQL 的临时正/负 Go package，用实际配置和固定 linter 分别检查。负例含 import/类型/变量别名、Table 方法值、Raw/Exec、AutoMigrate 和浮点转换，要求所有目标诊断出现；正例使用生成 Model 并读取 GORM 元数据，必须零问题。文件使用固定 Go fmt，结束清理，只针对明确 package 运行，不进入正常 ./... 源码。

policy-test 已加入 verify/CI，另有 Go 单元/race 覆盖 AST 的字符串/元数据对照、模型标签/字段/nullable 错误和嵌套 wire float。工具只增加已有 linter 能力及现有 Proto/标准库代码，无新依赖。验证见 [P03.3](validation/P03-03.md)。
