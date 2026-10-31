package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckRejectsDriftWithoutOverwritingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	arguments := []string{"-output", path}
	if err := run(arguments, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(append(arguments, "-check"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("drift\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run(append(arguments, "-check"), io.Discard); err == nil {
		t.Fatal("check accepted a changed contract")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "drift\n" {
		t.Fatalf("check mutated the document: %q, %v", data, err)
	}
	if err := run(append(arguments, "unexpected"), io.Discard); err == nil {
		t.Fatal("unexpected positional argument accepted")
	}
}
