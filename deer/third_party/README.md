# Proto dependencies

`lukla-go/` 是用户提供的 Himalaya Go SDK 原样依赖快照，模块名保留 `gitlab.com/himalaya/lukla-go`，通过根 go.mod 的本地 replace 引用。四个服务继续共用根应用 module。版本由 `lukla-go.snapshot.json` 的逐文件 SHA-256 固定；SDK 实现和生成文件不在 Muskdeer 中修改或重新生成，更新从上游 SDK 重新核对。源目录为 `Himalaya/sdk/go/lukla-go`，原始模块声明保留；应用开发、测试和构建仍使用根项目的 Go 1.27.3。

tagger/tagger.proto 与 tagger/LICENSE 原样取自 github.com/srikrsna/protoc-gen-gotag v1.0.2（MIT），用于在 Proto 字段上声明 GORM tag。生成插件版本固定于 tools/versions.json；生成的 Go 代码引用同版本模块的 tagger 包。

上游：[protoc-gen-gotag](https://github.com/srikrsna/protoc-gen-gotag/tree/v1.0.2)。

hertz/api.proto 与 hertz/LICENSE 原样取自 github.com/cloudwego/hertz/cmd/hz v0.9.8（Apache-2.0）。生成时通过明确的 go_package 映射把 HTTP 选项生成到 deer/pb/common/hertz；hz 使用 --use 复用项目 API DTO，不再生成第二套接口结构。
