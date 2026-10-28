# 管理运行请求与操作结果

P07.10提供真实数据库控制事务与结果观测仓储。当前页面和HTTP/gRPC仍只有查询；写接口、页面按钮及托管观测工作器继续后续步骤，不能据此认定完整A27完成。

`AdminStrategyControl.Control`要求持久operator/admin会话、独立CSRF、操作者范围内16–64字符幂等键、明确实例revision及非空原因。依次锁用户、会话、实例，在READ COMMITTED事务内重新检查权限/到期，比较revision，写请求目标、关闭旧风险许可、追加不可变审计及ACCEPTED操作记录。任一失败整体回滚；不修改实际运行状态、余额或订单，不自动创建账户/配置/风险许可。

支持strategy.start/resume/pause/stop四种明确意图。start只接受CREATED/STOPPED，resume只接受PAUSED，pause只接受RUNNING/STARTING/RECOVERING；stop允许已知生命周期状态。start/resume必须已有匹配代码及模板的完整typed配置。同一操作者/键/规范化业务内容返回原收据，即使实例revision已变化；同键不同内容或新键旧revision返回冲突。不能以取消/平仓替代停止语义。

请求事务只表示已受理。ACCEPTED记录绑定请求版本、来源、目标、实际产生的revision及审计；`Operation`在独立只读快照内重新验证viewer权限后读取，不隐式执行命令或观测。

`AdminOperationObserver.Pending/Observe`只更新操作结果：目标/配置/来源被替换或实例删除时记录SUPERSEDED；仍匹配时，实际状态达到目标才能记录SUCCEEDED。RUNNING还需实际生效配置/来源及当前正epoch租约owner一致且未到期；PAUSED/STOPPED需原OMS/核心订单/动作义务清空及策略预留归零。数据库触发器独立复核同一事实，记录观察到的revision、状态和owner/epoch。观测不创建控制器，不改变请求目标、实际状态、风险或资金。

操作身份、已完成事实和历史不可改删，delete_at字段保持可空且操作历史不得软删除。授权按用户行串行化键作用域，锁顺序为用户→会话→实例→操作；等待实例锁后再次依据数据库时钟检查会话，防止过期请求提交。异步观测可以由任一节点重复调用；尚未满足证明时继续保留ACCEPTED，不伪造成功或释放预留。

生成模型与迁移25见[模型Proto](../deer/proto/model/v1/admin.proto)及[数据库迁移](MIGRATIONS.md)。验证和当前交付边界见[P07.10](validation/P07-10.md)。
