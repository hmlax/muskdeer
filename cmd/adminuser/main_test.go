package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestProvisionCommandRequiresExplicitInputsAndNeverEchoesSecrets(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	t.Setenv("MUSKDEER_ADMIN_TEST_PASSWORD", "test-only-secret-should-not-be-echoed")
	for _, args := range [][]string{
		{},
		{"-config", "/not-present", "-username", "admin", "-role", "root", "-password-env", "MUSKDEER_ADMIN_TEST_PASSWORD"},
		{"-config", "/not-present", "-username", "admin", "-role", "admin", "-password-env", "bad-name"},
		{"-config", "/not-present", "-username", "admin", "-role", "admin", "-password-env", "MUSKDEER_ADMIN_TEST_PASSWORD"},
	} {
		var output, diagnostic bytes.Buffer
		if code := run(ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 || strings.Contains(diagnostic.String(), "test-only-secret") || strings.Contains(diagnostic.String(), "/not-present") {
			t.Fatal("invalid provisioning succeeded or exposed input")
		}
	}
}
