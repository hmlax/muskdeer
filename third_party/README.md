# Proto dependencies

tagger/tagger.proto 与 tagger/LICENSE 原样取自 github.com/srikrsna/protoc-gen-gotag v1.0.2（MIT），用于在 Proto 字段上声明 GORM tag。生成插件版本固定于 tools/versions.json；生成的 Go 代码引用同版本模块的 tagger 包。

上游：[protoc-gen-gotag](https://github.com/srikrsna/protoc-gen-gotag/tree/v1.0.2)。

hertz/api.proto 与 hertz/LICENSE 原样取自 github.com/cloudwego/hertz/cmd/hz v0.9.8（Apache-2.0）。生成时通过明确的 go_package 映射把 HTTP 选项生成到 deer/proto/hertz；hz 使用 --use 复用项目 API DTO，不再生成第二套接口结构。
