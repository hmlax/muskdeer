# 主工作目录与构建来源

本地开发、编译、测试和后续部署统一使用主工作目录。此前缓存worktree的已验证提交已合并回来，缓存目录只用于历史对照、依赖缓存和私有诊断证据，不再作为应用源码。

```sh
git rev-parse --show-toplevel
python3 scripts/go.py version
make compose-source
make generate-check fmt-check lint test
make build
make admin-deps admin-frontend-build admin-frontend-check
```

`make compose-source`只把现有`.cache/compose/compose.env`的`MUSKDEER_BUILD_CONTEXT`及本地准备状态中的`source`指向当前仓库，保留其余设置和凭据；它不访问数据库或Nacos、不启停容器、不迁移或创建账户。再次`compose-prepare`的默认来源也为当前仓库，旧的缓存来源不会自动复用。

`compose-prepare`、`bootstrap`和`compose-up`仍是开发环境初始化入口，不用于重新初始化已经运行的做市环境。现有实例的运行配置、绑定目录、资源及镜像选择应从当前部署核对保留，单独构建并更新指定服务。

原有管理员平仓原子意图改动继续保留为未提交状态，其中`0038_admin_flattening_intents.sql`不会因源码合并而应用到实际业务库。新增迁移和相关业务行为使用隔离测试库验证，运行库的历史和参数保持。

本次补齐的平仓事件关联校验、旧版本升级测试夹具修正也保留为未提交，不混入来源工具提交。并发迁移检查仍遇原2秒SQL超时；完整验证范围和未通过项见[当前进度](PROGRESS.md)。

两端前端的既有`.env.local`只作为私有本地运行配置原样保留，不进入Git。编译前保留原环境中的必要设置，不生成或替换部署密钥。

构建含未提交修改的工作树时，应保存源码清单及内容摘要，并在构建记录中明确标注工作树内容；提交SHA只能标识已提交部分。镜像摘要、构建参数及实际部署代次一并保留，不能仅凭可变标签判定来源。
