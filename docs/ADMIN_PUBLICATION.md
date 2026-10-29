# 受审计的参数版本发布与换源事务

P07.21提供`domain.AdminStrategyPublisher`与`repository.NewAdminStrategyPublisher`，借用已有数据库，构造无I/O/工作器。P07.22提供[StrategyPublication typed gRPC](../api/strategy/v1/publication.proto)及同源HTTP，页面继续后续步骤；[创建](ADMIN_CREATION.md)、[运行控制](ADMIN_CONTROL.md)与[激活预算](STRATEGY_LIFECYCLE.md)各自保持原有职责。

请求包含actor范围幂等键、strategy ID、expected_revision、expected_config_version、明确binance/okx、完整typed Maker/Grid参数及原因。ID/版本正整数并拒绝溢出，键16–64有界ASCII，原因trim后1–256 UTF-8字节；参数联合、decimal范围/巨大指数及HedgeEnabled门禁复用创建规则。模板必须保持一致，账户、instrument、code_version与LIVE/REPLAY模式来自现有实例，不能在发布请求中改写。

同一RC事务先锁当前用户/会话，重新检查operator/admin、CSRF、启用/credential/revocation及数据库到期时间；同一actor键串行。当前有效权限下，相同规范请求原键回放原收据，即使已发布后续版本；同键不同内容或任一expected值过期返回conflict。参数先编码/解码为本次事务拥有的具体typed值，再由确定性Proto及有长度边界的标量计算SHA，指纹不保存业务bytes/JSON包装。

新效果锁实例NO KEY UPDATE，再按instrument→account→账户预算→持仓锚点→指定feed共享校验已开通依赖，沿用创建的simulator/TRADING/启用/资产/每笔最小金额/对账与外部符号规则。缺所选模式/来源绑定不回退其他源。已有不可变参数版本下分配最大版本加一，不复用历史编号；新版本保持schema/template/code身份，created_by来自当前用户。

事务只推进requested版本/来源与实例revision；保持实际state、desired目标、active对、owner/epoch、原策略预算和所有资金/订单。关闭旧风险，当前未完成且绑定旧目标/配置/来源的控制收据同步SUPERSEDED，避免观测器遗漏中间替代。审计、不可变`AdminStrategyPublication`、新参数及requested修改同事务；SQL最终验证当前权限、匹配审计、新配置与关闭风险/旧控制替代证明。会话在依赖/收据等待期间到期、审计或任一步失败全部回滚，旧控制也不被永久替代。收据禁止改写、软删除、物理删除；原键回放仍须当前权限。

仍请求且实际RUNNING的实例在requested/active不一致期间，周期风控持久返回`CONFIGURATION_PENDING`并关闭风险，暂停/停止继续保持关闭；规划与OMS准入独立拒绝新增风险。最终核心SQL提交再次验证，旧写入方即使保留旧健康源/旧许可也无法新增订单。旧单观察、成交和撤单继续可用，资金预留直到实际终态确认才释放；发布回包只是请求元数据已提交，不能显示为新参数已激活、已恢复运行或撤单完成。

控制器继续原quiesce、所选健康行情、当前lease/恢复及最坏库存政策流程。首次激活/换active对时，P07.20才在清理旧义务且预留严格零后更新typed策略额度；暂停/停止目标保留，发布不隐式恢复。保存合法参数不承诺当前库存、新源健康或最终运行资格。

## gRPC 与 HTTP

strategy的`control_enabled`显式注册独立`StrategyPublication.PublishConfiguration`，与创建/运行控制借用同一数据库；API端沿`strategy_controls_enabled`借用已有strategy连接。查询模式不注册服务/POST，无新增连接、工作器、账户或资金。gRPC只接受API mTLS身份、当前session/独立CSRF/key，沿用5秒原生期限与2KiB消息边界、具体参数未知字段检查；403/401/400/409/404/503由固定业务失败映射，错误不回显原始原因。

`POST /api/v1/strategies/{strategy_id}/configurations`要求固定Origin、唯一Cookie、`X-CSRF-Token`和`Idempotency-Key`，Content-Type JSON且无query，body至多8192字节。实例ID由规范正整数路径确定，body不能覆盖；两个expected字段必须是规范JSON string，拒绝数字别名/前导零/溢出。完整ProtoJSON拒绝未知及重复字段，小数数字拒绝；会话不能来自body。

```json
{
  "expected_revision": "5",
  "expected_config_version": "2",
  "market_data_source": "okx",
  "parameters": {
    "grid": {
      "lower_price": "98.123456789012345678",
      "upper_price": "102",
      "grid_count": 5,
      "total_budget": "50",
      "per_order_notional": "10",
      "inventory_limit": "20",
      "max_market_age_ms": "500"
    }
  },
  "reason": "explicit configuration publication"
}
```

201与原键201回放均返回typed PublicationReceipt：id/actor_user_id/strategy_id、previous_config_version/config_version、previous_revision/revision、market_data_source/reason/created_at_ms；int64按ProtoJSON string输出，`Location`指向策略详情，所有响应no-store。API在展示成功前检查实例、两个版本推进、来源和规范原因与原请求匹配，并要求actor和时间为正值；不匹配视为不可确认的503，客户端保留原键/原请求观察或重试。409明确冲突、400无效请求，401/403仍由当前持久权限裁决；201仅证明requested输入的这次历史提交，后续详情可能已有更新请求。
