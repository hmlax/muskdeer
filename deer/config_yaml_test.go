package deer

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func yamlService(data string) string {
	return fmt.Sprintf("name: strategy\ninstance_id: yaml-test\nversion: test\nstartup_timeout: 5s\nshutdown_timeout: 5s\ndata_sha256: '%x'\n", sha256.Sum256([]byte(data)))
}

func TestConfigPairRejectsMixedDocumentsAndUnsafeYAML(t *testing.T) {
	base := "name: strategy\nversion: test\n"
	for _, data := range []string{
		base + "grpc: {address: '127.0.0.1:0'}\n",
		base + "nacos: {address: secret-bootstrap-override}\n",
		base + "name: strategy\n",
		base + "business: &reused {enabled: false}\n",
		base + "business: {venue: {quantity_fraction: 0.123456789012345678}}\n",
		base + "business: {venue: {quantity_fraction: .nan}}\n",
		base + "business: {venue: {depth: !!int null}}\n",
		base + "business: !unsupported {}\n",
		base + "---\nname: strategy\n",
		strings.Replace(base, "version: test", "version: another", 1),
		strings.Replace(base, "name: strategy", "name: execution", 1),
	} {
		if _, err := decodeConfigPair(t.Context(), []byte(data), []byte(yamlService(data))); err == nil || strings.Contains(err.Error(), "secret-bootstrap-override") {
			t.Fatal("invalid, unquoted decimal or foreign YAML accepted/exposed")
		}
	}
	for _, service := range []string{
		yamlService(base) + "postgres: {host: forbidden}\n",
		strings.Replace(yamlService(base), "data_sha256:", "other_hash:", 1),
		yamlService(base) + "instance_id: duplicate\n",
	} {
		if _, err := decodeConfigPair(t.Context(), []byte(base), []byte(service)); err == nil {
			t.Fatal("service.yaml accepted data fields or duplicate/unknown fields")
		}
	}
	if _, err := decodeConfigPair(t.Context(), []byte(base+"# changed bytes\n"), []byte(yamlService(base))); err == nil {
		t.Fatal("mixed publication accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := decodeConfigPair(ctx, []byte(base), []byte(yamlService(base))); !errors.Is(err, context.Canceled) {
		t.Fatalf("pair lost caller cancellation: %v", err)
	}
}

func TestConfigYAMLKeepsDecimalStringsAndTypedRoundtrip(t *testing.T) {
	data := "name: strategy\nversion: test\nbusiness:\n  venue:\n    quantity_fraction: '0.123456789012345678'\n"
	cfg, err := decodeConfigPair(t.Context(), []byte(data), []byte(yamlService(data)))
	if err != nil || cfg.GetBusiness().GetVenue().GetQuantityFraction() != "0.123456789012345678" {
		t.Fatalf("exact decimal changed: %v", err)
	}
	d, s, err := EncodeConfigPair(cfg)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := decodeConfigPair(t.Context(), d, s)
	if err != nil || !proto.Equal(cfg, roundtrip) {
		t.Fatalf("typed pair roundtrip changed settings: %v", err)
	}
	var fromJSON configv1.ServiceConfig
	if err := protojson.Unmarshal([]byte(nacosConfigFixture), &fromJSON); err != nil {
		t.Fatal(err)
	}
	d, s, err = EncodeConfigPair(&fromJSON)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err = decodeConfigPair(t.Context(), d, s)
	if err != nil || !proto.Equal(&fromJSON, roundtrip) {
		t.Fatalf("split changed process settings: %v", err)
	}
}
