package console

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
)

func TestAccountRegistrationExactIDsAndIdentityBeforeDatabase(t *testing.T) {
	t.Parallel()
	id, err := exactAccountID("9223372036854775807")
	if err != nil || id != 9223372036854775807 {
		t.Fatalf("exactAccountID(max int64) = %d, %v", id, err)
	}
	for _, input := range []string{"0", "-1", "01", "1e3", "9223372036854775808"} {
		if _, err := exactAccountID(input); err == nil {
			t.Fatalf("exactAccountID(%q) accepted invalid ID", input)
		}
	}
	server := &Server{controls: true}
	request := &pb.CreateAccountRequest{Actor: &pb.Actor{
		Subject:   "user:1",
		RequestId: "550e8400-e29b-41d4-a716-446655440000",
	}}
	if _, err := server.CreateAccount(t.Context(), request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("unverified registration = %v", err)
	}
	if _, err := server.CreateAccount(operatorContext(t.Context(), rpcsecurity.ServiceOperator), request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("platform registration = %v", err)
	}
}
