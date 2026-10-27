package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMigrationRequiresExplicitApplyBeforeConfigurationIO(t *testing.T) {
	for _, args := range [][]string{nil, {"-config", "/not-read.json"}, {"-apply"}, {"-apply", "-config", "/not-read.json", "extra"}} {
		var output, diagnostic bytes.Buffer
		if code := run(t.Context(), args, &output, &diagnostic); code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "requires -apply and -config") {
			t.Fatalf("arguments %v = %d %s %s", args, code, &output, &diagnostic)
		}
	}
}
