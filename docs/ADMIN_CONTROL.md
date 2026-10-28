# 管理运行请求与操作结果

P07.10提供真实数据库控制事务与结果观测仓储，P07.11增加显式启用的typed控制gRPC与Deer托管观测工作器。当前页面和HTTP仍只有查询；写HTTP、页面按钮及完整浏览器闭环继续后续步骤，不能据此认定完整A27完成。

`AdminStrategyControl.Control`要求持久operator/admin会话、独立CSRF、操作者范围内16–64字符幂等键、明确实例revision及非空原因。依次锁用户、会话、实例，在READ COMMITTED事务内重新检查权限/到期，比较revision，写请求目标、关闭旧风险许可、追加不可变审计及ACCEPTED操作记录。任一失败整体回滚；不修改实际运行状态、余额或订单，不自动创建账户/配置/风险许可。

支持strategy.start/resume/pause/stop四种明确意图。start只接受CREATED/STOPPED，resume只接受PAUSED，pause只接受RUNNING/STARTING/RECOVERING；stop允许已知生命周期状态。start/resume必须已有匹配代码及模板的完整typed配置。同一操作者/键/规范化业务内容返回原收据，即使实例revision已变化；同键不同内容或新键旧revision返回冲突。不能以取消/平仓替代停止语义。

请求事务只表示已受理。ACCEPTED记录绑定请求版本、来源、目标、实际产生的revision及审计；`Operation`在独立只读快照内重新验证viewer权限后读取，不隐式执行命令或观测。

管理请求改变目标时，同一事务立即将该实例中目标/配置/来源不再匹配的旧ACCEPTED收据记为SUPERSEDED，并绑定替换时的实际状态/revision/owner。这保留了两次观测之间“启动→停止→启动”的中间事实；旧启动不能被最后一次启动复活。同目标请求可以共享随后达到该目标的事实，但各自保留审计、幂等键及版本。新审计或新收据失败时，旧收据的替代状态也整体回滚，见[P07.12](validation/P07-12.md)。

`AdminOperationObserver.Pending/Observe`只更新操作结果：目标/配置/来源被替换或实例删除时记录SUPERSEDED；仍匹配时，实际状态达到目标才能记录SUCCEEDED。RUNNING还需实际生效配置/来源及当前正epoch租约owner一致且未到期；PAUSED/STOPPED需原OMS/核心订单/动作义务清空及策略预留归零。数据库触发器独立复核同一事实，记录观察到的revision、状态和owner/epoch。观测不创建控制器，不改变请求目标、实际状态、风险或资金。

操作身份、已完成事实和历史不可改删，delete_at字段保持可空且操作历史不得软删除。授权按用户行串行化键作用域，锁顺序为用户→会话→实例→操作；等待实例锁后再次依据数据库时钟检查会话，防止过期请求提交。异步观测可以由任一节点重复调用；尚未满足证明时继续保留ACCEPTED，不伪造成功或释放预留。

生成模型与迁移25见[模型Proto](../deer/proto/model/v1/admin.proto)及[数据库迁移](MIGRATIONS.md)。验证和当前交付边界见[P07.10](validation/P07-10.md)。

## 显式控制gRPC与结果工作器

`business.strategy.management_enabled=true`继续只开启原查询服务。额外`control_enabled=true`才注册独立StrategyControl服务（ControlStrategy/GetOperation）及`strategy.operations`工作器；control要求management已开且允许api.grpc。账户为空时仍不允许交易lease/outbound clients；可以只托管控制事务和结果观测，不创建执行者/资金/风险许可。没有账户执行者时启动请求会继续ACCEPTED，不能仅因控制节点在线记为RUNNING。

两个RPC要求已验证api.grpc mTLS身份、原生期限不超过5秒、请求不超过2KiB、无未知字段及规范会话。ControlStrategy还要求独立规范CSRF、明确动作/原因/幂等键和revision，持久operator权限由实际事务检查；GetOperation仅要求持久viewer。冲突为Aborted，非法输入/未认证/无权限/不存在/依赖故障分别保留固定分类，公开错误不包含数据库原因或提交的秘密。

响应为显式StrategyOperation及可为空的OperationObservation，ACCEPTED没有完成时间/观察事实；终态返回持久观察revision、实际状态和owner/epoch。Proto的ID使用int64无损传递；后续HTTP序列化的int64和所有小数使用字符串，不用JSON/map转换桥。

观测工作器按ID每页最多16条轮询，未完成的早期操作不能阻塞后续页；每条Pending/Observe独立取得原生operation_timeout并服从Deer父取消，不共享耗尽预算。游标只用于公平调度，重启从头恢复；多节点可重复运行，事务锁/CAS及SQL终态不可变约束决定结果。工作器不执行交易命令或改风险/余额，依赖故障按现有Deer工作器规则撤销就绪并停止。配置、真实多进程和边界验证见[P07.11](validation/P07-11.md)。
