package frameworkprobe

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/go-kratos/kratos/v2"
	kratosgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/shopspring/decimal"
	"go.uber.org/goleak"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	probev1 "github.com/hmlax/muskdeer/api/probe/v1"
)

const (
	probeServiceName       = "muskdeer-framework-probe"
	invalidPriceMessage    = "invalid price"
	invalidQuantityMessage = "invalid quantity"
)

type arithmeticProbe struct {
	probev1.UnimplementedArithmeticProbeServer
}

func (s *arithmeticProbe) Multiply(ctx context.Context, request *probev1.MultiplyRequest) (*probev1.MultiplyReply, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	price, err := decimal.NewFromString(request.GetPrice())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, invalidPriceMessage)
	}
	quantity, err := decimal.NewFromString(request.GetQuantity())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, invalidQuantityMessage)
	}
	return &probev1.MultiplyReply{Notional: price.Mul(quantity).String()}, nil
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestKratosGeneratedGRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	listener := bufconn.Listen(1024 * 1024)
	rpc := kratosgrpc.NewServer(
		kratosgrpc.Listener(listener),
		kratosgrpc.Endpoint(&url.URL{
			Scheme: "grpc",
			Host:   "bufconn",
		}),
		kratosgrpc.DisableReflection(),
	)
	probev1.RegisterArithmeticProbeServer(rpc, &arithmeticProbe{})
	started := make(chan struct{})
	application := kratos.New(
		kratos.Context(ctx),
		kratos.Name(probeServiceName),
		kratos.Server(rpc),
		kratos.StopTimeout(time.Second),
		kratos.AfterStart(func(context.Context) error {
			close(started)
			return nil
		}),
	)
	done := make(chan error, 1)
	go func() { done <- application.Run() }()
	t.Cleanup(func() {
		if err := application.Stop(); err != nil {
			t.Errorf("Kratos.Stop() = %v", err)
		}
		stopped, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 2*time.Second)
		defer cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Kratos.Run() = %v", err)
			}
		case <-stopped.Done():
			t.Error("Kratos.Run() did not stop within the shutdown bound")
		}
	})
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatalf("Kratos start = %v", ctx.Err())
	}
	connection, err := grpc.NewClient("passthrough:///probe",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Errorf("gRPC connection.Close() = %v", err)
		}
	})
	client := probev1.NewArithmeticProbeClient(connection)
	result, err := client.Multiply(ctx, &probev1.MultiplyRequest{
		Price:    "0.1",
		Quantity: "0.2",
	})
	if err != nil || result.GetNotional() != "0.02" {
		t.Fatalf("gRPC Multiply(0.1,0.2) = %v, %v; want 0.02", result, err)
	}
	_, err = client.Multiply(ctx, &probev1.MultiplyRequest{
		Price:    "invalid",
		Quantity: "0.2",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("gRPC invalid price = %v, want InvalidArgument", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	_, err = client.Multiply(canceled, &probev1.MultiplyRequest{})
	if status.Code(err) != codes.Canceled {
		t.Errorf("gRPC canceled request = %v, want Canceled", err)
	}
}

func TestHertzDecimalJSON(t *testing.T) {
	engine := server.New()
	service := &arithmeticProbe{}
	engine.GET("/probe/notional", func(ctx context.Context, request *app.RequestContext) {
		var input probev1.MultiplyRequest
		if err := request.BindAndValidate(&input); err != nil {
			request.String(consts.StatusBadRequest, "%s", err.Error())
			return
		}
		result, err := service.Multiply(ctx, &input)
		if err != nil {
			request.String(consts.StatusBadRequest, "%s", err.Error())
			return
		}
		request.JSON(consts.StatusOK, result)
	})
	response := ut.PerformRequest(engine.Engine, "GET", "/probe/notional?price=0.1&quantity=0.2", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("Hertz status = %d, body %q; want 200", response.StatusCode(), response.Body())
	}
	var output struct {
		Notional string `json:"notional"`
	}
	if err := json.Unmarshal(response.Body(), &output); err != nil {
		t.Fatalf("Hertz decimal string JSON = %v", err)
	}
	if output.Notional != "0.02" {
		t.Errorf("Hertz Multiply(0.1,0.2) = %q, want 0.02", output.Notional)
	}
	invalid := ut.PerformRequest(engine.Engine, "GET", "/probe/notional?price=invalid&quantity=0.2", nil).Result()
	if invalid.StatusCode() != consts.StatusBadRequest {
		t.Errorf("Hertz invalid price status = %d, want 400", invalid.StatusCode())
	}
}
