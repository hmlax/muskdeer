package transport

import (
	"regexp"

	"github.com/cloudwego/hertz/pkg/app"
)

type Binding struct {
	Operation string
	Stream    bool
	Handler   app.HandlerFunc
}

var hzParameter = regexp.MustCompile(`(^|/):([A-Za-z_][A-Za-z0-9_]*)`)

// RegisterHZ keeps the original protobuf path template in authorization and
// audit metadata while accepting the :id syntax emitted by hz. Router also
// preserves literal RPC suffixes such as /users:exists and /tasks/:id:run.
func (r *Router) RegisterHZ(method, path string, binding Binding) {
	r.Register(method, hzParameter.ReplaceAllString(path, "$1{$2}"), binding.Operation, binding.Stream, binding.Handler)
}
