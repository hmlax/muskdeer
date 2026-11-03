package orderclient

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MetadataErrorCode is the gRPC metadata key for stable 4-digit error codes (see proto ErrorCode).
const MetadataErrorCode = "error-code"

var messageCodeRE = regexp.MustCompile(`^\[E(\d+)\]\s`)

// RPCError wraps a failed gRPC call with the order-service error code when present.
type RPCError struct {
	ErrorCode uint32
	Message   string
	Code      codes.Code
}

func (e *RPCError) Error() string {
	if e.ErrorCode != 0 {
		return fmt.Sprintf("order-service rpc %s [E%d]: %s", e.Code, e.ErrorCode, e.Message)
	}
	return fmt.Sprintf("order-service rpc %s: %s", e.Code, e.Message)
}

// GRPCStatus preserves standard gRPC status inspection after wrapping.
func (e *RPCError) GRPCStatus() *status.Status {
	return status.New(e.Code, e.Message)
}

// ErrorCodeFrom extracts a 4-digit code from *RPCError or from `[Ennnn]` message prefix.
func ErrorCodeFrom(err error) uint32 {
	var re *RPCError
	if errors.As(err, &re) && re.ErrorCode != 0 {
		return re.ErrorCode
	}
	st, ok := status.FromError(err)
	if !ok {
		return 0
	}
	return parseCodeFromMessage(st.Message())
}

// AsRPCError converts a gRPC status into *RPCError when possible.
func AsRPCError(err error) (*RPCError, bool) {
	if err == nil {
		return nil, false
	}
	var re *RPCError
	if errors.As(err, &re) {
		return re, true
	}
	st, ok := status.FromError(err)
	if !ok {
		return nil, false
	}
	return &RPCError{
		ErrorCode: parseCodeFromMessage(st.Message()),
		Message:   st.Message(),
		Code:      st.Code(),
	}, true
}

func parseCodeFromMessage(msg string) uint32 {
	m := messageCodeRE.FindStringSubmatch(msg)
	if len(m) < 2 {
		return 0
	}
	n, err := strconv.ParseUint(m[1], 10, 32)
	if err != nil {
		return 0
	}
	return uint32(n)
}

func wrapGRPCError(err error) error {
	if err == nil {
		return nil
	}
	if re, ok := AsRPCError(err); ok {
		return re
	}
	return err
}
