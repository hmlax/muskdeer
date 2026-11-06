package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyFileRejectsNacosConnectionsAndAmbiguousInput(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{}`,
		`{"policies":[]}`,
		`{"policies":[{"service_name":"execution","parameters":{"enabled":false,"venue_connection":{"target":"localhost:8080"}}}]}`,
		`{"policies":[{"service_name":"execution","service_name":"market","parameters":{}}]}`,
		`{"policies":[{"service_name":"execution","parameters":{"venue":{"quantity_fraction":0.1}}}]}`,
		`{"policies":[{"service_name":"execution","parameters":{}}]} {}`,
		strings.Repeat(" ", maximumPolicyBytes+1),
	} {
		path := filepath.Join(t.TempDir(), "policies.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readPolicies(path); err == nil {
			t.Fatal("invalid maintenance input accepted")
		}
	}
}

func TestPolicyFilePreservesRevisionAndExactDecimals(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "policies.json")
	body := `{"policies":[{"service_name":"strategy","expected_revision":"9007199254740993","parameters":{"enabled":false,"venue":{"quantity_fraction":"0.123456789012345678"}}}]}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := readPolicies(path)
	if err != nil {
		t.Fatal(err)
	}
	if input.Policies[0].ExpectedRevision != 9007199254740993 || input.Policies[0].Parameters.Venue.QuantityFraction != "0.123456789012345678" {
		t.Fatal("maintenance parser rounded a database value")
	}
}
