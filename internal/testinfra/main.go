// Command testinfra cleans resources in the dedicated local test NATS account.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	natsjs "github.com/nats-io/nats.go/jetstream"
)

func main() {
	if err := clean(); err != nil {
		fmt.Fprintln(os.Stderr, "dedicated test account cleanup failed")
		os.Exit(1)
	}
}

func clean() error {
	if os.Getenv("MUSKDEER_TEST_NATS_USERNAME") != "muskdeer-test" || os.Getenv("MUSKDEER_TEST_NATS_PASSWORD") == "" {
		return nats.ErrAuthorization
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := nats.Connect("nats://"+os.Getenv("MUSKDEER_TEST_NATS_ADDRESS"),
		nats.UserInfo(os.Getenv("MUSKDEER_TEST_NATS_USERNAME"), os.Getenv("MUSKDEER_TEST_NATS_PASSWORD")),
		nats.NoReconnect(), nats.Timeout(time.Second))
	if err != nil {
		return err
	}
	defer conn.Close()
	js, err := natsjs.New(conn)
	if err != nil {
		return err
	}
	names := js.StreamNames(ctx)
	for name := range names.Name() {
		// Only streams in the new test account and project subject namespace
		// belong to this runner. Never clean the shared global account.
		stream, err := js.Stream(ctx, name)
		if err != nil {
			return err
		}
		for _, subject := range stream.CachedInfo().Config.Subjects {
			if !strings.HasPrefix(subject, "muskdeer.") {
				return nats.ErrAuthorization
			}
		}
		if err := js.DeleteStream(ctx, name); err != nil {
			return err
		}
	}
	return names.Err()
}
