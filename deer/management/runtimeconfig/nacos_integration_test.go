//go:build integration

package runtimeconfig

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/registry"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type nacosTestService struct {
	operatorV1.UnimplementedGatewayServiceServer
}

func (*nacosTestService) GetPolicies(context.Context, *emptypb.Empty) (*operatorV1.PoliciesResponse, error) {
	return &operatorV1.PoliciesResponse{Policies: []*operatorV1.RolePolicy{{Role: "nacos-discovery-verified"}}}, nil
}

// TestNacosConfigurationAndDiscovery creates only a unique temporary group and
// registration, and removes both. Point NACOS_TEST_ADDR at an isolated test server.
func TestNacosConfigurationAndDiscovery(t *testing.T) {
	address := os.Getenv("NACOS_TEST_ADDR")
	if address == "" {
		t.Skip("set NACOS_TEST_ADDR to run the Nacos integration test")
	}
	t.Setenv("NACOS_ADDR", address)
	t.Setenv("NACOS_USERNAME", os.Getenv("NACOS_TEST_USERNAME"))
	t.Setenv("NACOS_PASSWORD", os.Getenv("NACOS_TEST_PASSWORD"))
	t.Setenv("NACOS_NAMESPACE_ID", os.Getenv("NACOS_TEST_NAMESPACE_ID"))
	t.Setenv("NACOS_GROUP", fmt.Sprintf("GO_ADMIN_TEST_%d", time.Now().UnixNano()))
	param, err := nacosParam()
	if err != nil {
		t.Fatal(err)
	}
	config, err := clients.NewConfigClient(param)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(config.CloseClient)
	for dataID, content := range map[string]string{"server.yaml": testServerConfig, "data.yaml": testDataConfig} {
		configParam := vo.ConfigParam{
			DataId:  dataID,
			Group:   nacosEnv("NACOS_GROUP", ""),
			Content: content,
		}
		published, err := config.PublishConfig(configParam)
		if err != nil || !published {
			t.Fatalf("publish %s: published=%v error=%v", dataID, published, err)
		}
		t.Cleanup(func() {
			if _, err := config.DeleteConfig(configParam); err != nil {
				t.Errorf("delete %s: %v", dataID, err)
			}
		})
	}
	cfg, registrar, naming, err := Load("operator")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(naming.CloseClient)
	if cfg.GetServer().GetGrpc().GetAddr() != ":7790" {
		t.Fatal("startup did not use Nacos configuration")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := kgrpc.NewServer(kgrpc.Listener(listener))
	operatorV1.RegisterGatewayServiceServer(server, &nacosTestService{})
	done := make(chan error, 1)
	go func() { done <- server.Start(context.Background()) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server stopped with error: %v", err)
			}
		case <-ctx.Done():
			t.Error("Nacos fixture gRPC server did not stop")
		}
	})
	name := fmt.Sprintf("go-admin-test-operator-%d", time.Now().UnixNano())
	instance := &registry.ServiceInstance{
		ID:        name,
		Name:      name,
		Version:   "test",
		Endpoints: []string{"grpc://" + listener.Addr().String()},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := registrar.Register(ctx, instance); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := registrar.Deregister(ctx, instance); err != nil {
			t.Errorf("deregister Nacos fixture: %v", err)
		}
	})
	// The upstream Nacos registry publishes a protocol suffix on service names.
	conn, err := kgrpc.DialInsecure(ctx, kgrpc.WithEndpoint("discovery:///"+name+".grpc"), kgrpc.WithDiscovery(registrar.(registry.Discovery)), kgrpc.WithOptions(grpc.WithDefaultCallOptions(grpc.WaitForReady(true))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	response, err := operatorV1.NewGatewayServiceClient(conn).GetPolicies(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetPolicies()) != 1 || response.GetPolicies()[0].GetRole() != "nacos-discovery-verified" {
		t.Fatal("gRPC call did not reach the Nacos-discovered instance")
	}
}
