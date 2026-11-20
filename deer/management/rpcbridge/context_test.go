package rpcbridge

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/hmlax/muskdeer/deer/management/httpmeta"
	"github.com/stretchr/testify/require"
)

type bridgeTestHeader struct{ http.Header }

func (h bridgeTestHeader) Keys() []string {
	keys := make([]string, 0, len(h.Header))
	for key := range h.Header {
		keys = append(keys, key)
	}
	return keys
}

type bridgeTestTransport struct{ headers bridgeTestHeader }

func (t bridgeTestTransport) Kind() transport.Kind            { return transport.KindGRPC }
func (t bridgeTestTransport) Endpoint() string                { return "grpc://operator" }
func (t bridgeTestTransport) Operation() string               { return "/operator.v1.AuthenticationService/Login" }
func (t bridgeTestTransport) RequestHeader() transport.Header { return t.headers }
func (t bridgeTestTransport) ReplyHeader() transport.Header   { return t.headers }

func TestHTTPUserAgentSurvivesGRPCReservedMetadata(t *testing.T) {
	const browserAgent = "MerchantBrowser/1.0"
	request, err := http.NewRequest(http.MethodPost, "http://admin/admin/v1/login", nil)
	require.NoError(t, err)
	request.Header.Set("User-Agent", browserAgent)
	request.Header.Set(gatewayUserAgentKey, "untrusted browser metadata")
	outbound := bridgeTestTransport{
		headers: bridgeTestHeader{
			Header: make(http.Header),
		},
	}
	ctx := httpmeta.NewContext(context.Background(), &httpmeta.Exchange{
		RequestValue: request,
	})
	ctx = transport.NewClientContext(ctx, outbound)
	_, err = Client()(func(context.Context, any) (any, error) { return nil, nil })(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, browserAgent, outbound.headers.Get(gatewayUserAgentKey))
	// The actual gRPC transport owns user-agent; it must not replace the browser's identity.
	outbound.headers.Set("User-Agent", "grpc-go/test")
	ctx = transport.NewServerContext(context.Background(), outbound)
	_, err = Server()(func(ctx context.Context, _ any) (any, error) {
		tr, ok := transport.FromServerContext(ctx)
		require.True(t, ok)
		require.Equal(t, browserAgent, tr.RequestHeader().Get("User-Agent"))
		httpTransport, ok := tr.(khttp.Transporter)
		require.True(t, ok)
		require.Equal(t, browserAgent, httpTransport.Request().UserAgent())
		return nil, nil
	})(ctx, nil)
	require.NoError(t, err)
}
