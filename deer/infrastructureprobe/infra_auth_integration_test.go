//go:build integration

package infrastructureprobe

import (
	"os"

	"github.com/nats-io/nats.go"
)

func testNATSAuth() nats.Option {
	if username := os.Getenv("MUSKDEER_TEST_NATS_USERNAME"); username != "" {
		return nats.UserInfo(username, os.Getenv("MUSKDEER_TEST_NATS_PASSWORD"))
	}
	return nats.Token(os.Getenv("MUSKDEER_TEST_NATS_TOKEN"))
}
func testNATSTokenEnv() string {
	if os.Getenv("MUSKDEER_TEST_NATS_USERNAME") != "" {
		return ""
	}
	return "MUSKDEER_TEST_NATS_TOKEN"
}
func testNATSPasswordEnv() string {
	if os.Getenv("MUSKDEER_TEST_NATS_USERNAME") != "" {
		return "MUSKDEER_TEST_NATS_PASSWORD"
	}
	return ""
}
