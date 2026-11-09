package console

import (
	"testing"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGatewayMetadataIsPlatformOnlyAndOmitsAuthentication(t *testing.T) {
	s := &Server{connection: &config.VenueConnection{
		Target:   "matching.invalid:9000",
		TokenEnv: "PRIVATE_GATEWAY_CREDENTIAL",
	}}
	ctx := operatorContext(t.Context(), rpcsecurity.ServiceOperator)
	merchant := &pb.Actor{
		Subject:  "user:7",
		TenantId: 7,
	}
	if _, err := s.ListGateways(ctx, &pb.ActorRequest{Actor: merchant}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("merchant gateway lookup: %v", err)
	}
	page, err := s.ListGateways(ctx, &pb.ActorRequest{Actor: &pb.Actor{Subject: "user:1"}})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Target != s.connection.Target || !page.Items[0].Configured {
		t.Fatal("gateway metadata does not match loaded infrastructure")
	}
	fields := page.Items[0].ProtoReflect().Descriptor().Fields()
	if fields.ByName("token_env") != nil || fields.ByName("token") != nil {
		t.Fatal("gateway contract must not expose authentication fields")
	}
}
