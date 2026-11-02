package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMigrationRequiresExplicitApplyBeforeConfigurationIO(t *testing.T) {
	for _, args := range [][]string{nil, {"-config", "/not-read.json"}, {"-apply", "-config", "/not-read.json", "extra"}} {
		var output, diagnostic bytes.Buffer
		if code := run(t.Context(), args, &output, &diagnostic); code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "requires -apply") {
			t.Fatalf("arguments %v = %d %s %s", args, code, &output, &diagnostic)
		}
	}
}

func TestMigrationWithoutFileRequiresNacosEnvironment(t *testing.T) {
	t.Setenv("NACOS_ADDRESS", "")
	var output, diagnostic bytes.Buffer
	if code := run(t.Context(), []string{"-apply"}, &output, &diagnostic); code != 1 || output.Len() != 0 {
		t.Fatal("migration accepted an absent Nacos bootstrap or reported success")
	}
}
