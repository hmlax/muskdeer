package rpcclient

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const messageServerStreamOnly = "RPC subscription requires a server-only stream"

// Subscribe owns one authenticated read stream until consume returns or Close
// cancels it. Long-lived subscriptions do not occupy unary admission slots.
func (f *Factory) Subscribe(ctx context.Context, target Target, consume func(context.Context, grpc.ClientConnInterface) error) (result error) {
	if ctx == nil || consume == nil {
		return errors.New(messageServerStreamOnly)
	}
	if _, err := f.Client(target); err != nil {
		return err
	}
	work, cancel := context.WithCancel(ctx)
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		cancel()
		return ErrClosed
	}
	f.sequence++
	id := f.sequence
	f.active[id] = cancel
	f.wait.Add(1)
	f.mu.Unlock()
	defer func() {
		result = contextualError(work, result)
		cancel()
		f.mu.Lock()
		delete(f.active, id)
		f.mu.Unlock()
		f.wait.Done()
	}()
	setup, end := context.WithTimeout(work, f.cfg.ConnectTimeout.AsDuration()+f.cfg.RequestTimeout.AsDuration())
	conn, err := f.transport(setup, target)
	if err == nil {
		err = f.waitReady(setup, target, conn)
	}
	if err != nil {
		end()
		return err
	}
	end()
	return consume(work, &subscriptionConnection{
		conn:        conn,
		maxRequest:  int(f.cfg.MaxRequestBytes),
		maxResponse: int(f.cfg.MaxResponseBytes),
	})
}

type subscriptionConnection struct {
	conn        *grpc.ClientConn
	maxRequest  int
	maxResponse int
}

func (*subscriptionConnection) Invoke(context.Context, string, any, any, ...grpc.CallOption) error {
	return status.Error(codes.Unimplemented, messageServerStreamOnly)
}

func (c *subscriptionConnection) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, options ...grpc.CallOption) (grpc.ClientStream, error) {
	if desc == nil || desc.ClientStreams || !desc.ServerStreams {
		return nil, status.Error(codes.Unimplemented, messageServerStreamOnly)
	}
	options = append(options, grpc.MaxRetryRPCBufferSize(0), grpc.MaxCallSendMsgSize(c.maxRequest), grpc.MaxCallRecvMsgSize(c.maxResponse), grpc.WaitForReady(false))
	return c.conn.NewStream(ctx, desc, method, options...)
}
