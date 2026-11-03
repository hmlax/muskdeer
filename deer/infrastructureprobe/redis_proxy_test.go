//go:build integration

package infrastructureprobe

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// delayedRedis delays only the fixture's replies from the real shared Redis.
// It never pauses the server or changes its configuration or other clients.
func delayedRedis(t *testing.T, address string) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	var blocked atomic.Bool
	var group sync.WaitGroup
	var mu sync.Mutex
	var connections []net.Conn
	register := func(connection net.Conn) bool {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil {
			if err := connection.Close(); err != nil {
				t.Log("closed fixture connection:", err)
			}
			return false
		}
		connections = append(connections, connection)
		return true
	}
	group.Go(func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			if !register(client) {
				return
			}
			group.Go(func() {
				defer closeRedisProxy(t, client)
				upstream, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", address)
				if err != nil {
					return
				}
				if !register(upstream) {
					return
				}
				defer closeRedisProxy(t, upstream)
				replies := make(chan struct{})
				go func() {
					defer close(replies)
					defer closeRedisProxy(t, client)
					buffer := make([]byte, 32*1024)
					for {
						count, err := upstream.Read(buffer)
						if count > 0 {
							if blocked.Load() {
								timer := time.NewTimer(400 * time.Millisecond)
								select {
								case <-ctx.Done():
									timer.Stop()
									return
								case <-timer.C:
								}
							}
							if _, writeErr := client.Write(buffer[:count]); writeErr != nil {
								return
							}
						}
						if err != nil {
							return
						}
					}
				}()
				_, _ = io.Copy(upstream, client)
				// The fixture owns both sockets; closing upstream joins its reader even
				// when the caller has already timed out and discarded the client socket.
				if err := upstream.Close(); err != nil {
					t.Log("closed fixture upstream:", err)
				}
				<-replies
			})
		}
	})
	t.Cleanup(func() {
		cancel()
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
		mu.Lock()
		for _, connection := range connections {
			if err := connection.Close(); err != nil && !isClosedConnection(err) {
				t.Error(err)
			}
		}
		mu.Unlock()
		group.Wait()
	})
	return listener.Addr().String(), func() { blocked.Store(true) }
}

func isClosedConnection(err error) bool { return errors.Is(err, net.ErrClosed) }

func closeRedisProxy(t *testing.T, connection net.Conn) {
	if err := connection.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Error(err)
	}
}
