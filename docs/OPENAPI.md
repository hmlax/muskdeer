# 管理 OpenAPI 合同

[openapi.json](openapi.json)描述现有Hertz管理边界：OpenAPI固定3.1.0，29个路径、31个GET/POST操作，65个具体schema。文档不包含服务器地址或凭据；尚未增加在线下载路由。可操作管理页面及其浏览器验收另见[管理入口](ADMIN_HTTP.md)，文档不能代替R08/A27操作验收。

## 生成与检查

`make openapi-generate`在contract-check之后从当前Proto运行时描述符及明确的HTTP元数据生成文档；`make openapi-check`比较确定性字节，只读检查不覆盖差异。`make generate`和`make generate-check`也包含该步骤，因此现有verify/CI门禁会检查生成物。新增接口必须同步维护目录；测试直接解析Hertz实际注册，拒绝缺失、额外路由或重复operationId。

响应引用现有api/strategy及api/admin的具体公开消息，递归引用公共参数Proto；没有为文档复制业务DTO。输入只引用公开Input消息或HTTP边界的明确字段，不输出内部gRPC AdminSession/CSRF/幂等包装，也不遍历整个服务描述符。新增不支持的字段类型或map会使生成失败，要求人工明确合同。

所有生成器/检查使用Go1.27.3。官方固定schema保存在[third_party](../third_party/README.md)，测试核对SHA并用固定v6.0.2验证器离线校验完整文档、编译业务schema；未知外部引用必须失败。该依赖只用于测试，不加入业务运行路径。

## 传输与权限

- int64 ID、修订、计数、epoch及毫秒时间均为规范十进制JSON字符串，范围在描述中明确；测试注册muskdeer-int64格式断言，覆盖有符号上下界及高于JavaScript安全整数的值。通用客户端还必须执行int64范围校验，不能只依赖自定义format。int32计数仍是JSON整数。
- 金额、价格、数量及BPS等小数均为字符串，具体输入精度在参数schema中描述。服务端使用decimal及业务精度/预算/时间/参数约束；JSON Schema不代替这些跨字段检查。本文档给出规范snake_case输入形态，ProtoJSON兼容形式不作为客户端推荐合同。
- 输出与实际UseProtoNames/EmitUnpopulated一致：普通消息字段存在且可为null，重复字段为数组；Proto optional标量不存在时省略，出现时不接受null。Maker/Grid参数只能选一个具体模板。
- secureSession与localSession是按部署二选一的Cookie鉴权方案。除login外需当前持久会话；写操作需固定Origin、独立X-CSRF-Token，业务写另需Idempotency-Key。UNBLOCK额外要求管理员和当前配置/目标前置字段；BLOCK必须省略这两个字段。
- x-muskdeer-feature明确启用条件，未配置路由404；GetOperation虽为viewer读取，仍随controls启用。x-muskdeer-minimum-role只是最低角色描述，不能替代当前会话和业务事务授权。所有业务查询的路径、筛选、游标、页长及服务观测的独立1–4页长均显式列出。
- 201元数据收据不表示启动或生效；202只表示操作/撤单已接受。200可能是幂等重放的历史终态。Stop、撤单与平仓语义分别保持，UNKNOWN预留不释放，当前没有平仓接口。读取服务readiness/租约只返回采样观测，不授予交易权限。

重复字段、未知名称、UTF-8字节限制、NUL、重复/未知查询、固定Host/Origin、撤销会话和最终资金/所有权门禁由实际服务校验，不能用静态文档验证替代。合同测试的Proto零值只验证传输形态，不代表零值业务回复可以通过现有HTTP成功响应校验。

验证记录见[P07.56](validation/P07-56.md)。后续增加受鉴权的在线文档入口和原生HTTP响应合同核验，再继续剩余P07及P08–P10；P11真实合同/环境仍独立阻塞。
