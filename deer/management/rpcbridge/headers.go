package rpcbridge

import (
	"context"

	"github.com/go-kratos/kratos/v2/transport"
	"github.com/hmlax/muskdeer/deer/management/httpmeta"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ReplyHeaders carries response metadata back to the HTTP gateway, including
// multiple Set-Cookie values. Kratos client transport does not collect replies.
func ReplyHeaders() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		var header metadata.MD
		opts = append(opts, grpc.Header(&header))
		err := invoke(ctx, method, req, reply, cc, opts...)
		if exchange, ok := httpmeta.FromContext(ctx); ok {
			for _, key := range []string{"set-cookie", "x-audit-username"} {
				for _, value := range header.Get(key) {
					exchange.Response.Add(key, value)
				}
			}
			return err
		}
		if inbound, ok := transport.FromServerContext(ctx); ok && inbound.ReplyHeader() != nil {
			for _, key := range []string{"set-cookie", "x-audit-username"} {
				for _, value := range header.Get(key) {
					inbound.ReplyHeader().Add(key, value)
				}
			}
		}
		return err
	}
}
