package deer

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
)

func TestNacosRejectsAllDatabaseExecutionParameters(t *testing.T) {
	for _, business := range []string{"{}", "{enabled: false}", "{venue: {depth: 20, quantity_fraction: '0.1'}}", "{strategy: {account_ids: [account]}}"} {
		data := "name: strategy\nversion: test\nbusiness: " + business + "\n"
		if _, err := decodeConfigPair(t.Context(), []byte(data), []byte(yamlService(data))); err == nil {
			t.Fatalf("Nacos accepted database execution parameters: %s", business)
		}
	}
	if (&configv1.DataConfig{}).ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name("business")) != nil {
		t.Fatal("Nacos data contract still exposes business parameters")
	}
}

func TestNacosEncoderRejectsResolvedBusinessSnapshot(t *testing.T) {
	cfg := &configv1.ServiceConfig{
		Name:     "strategy",
		Version:  "test",
		Business: &configv1.BusinessRuntime{},
	}
	if _, _, err := EncodeConfigPair(cfg); err == nil {
		t.Fatal("database policy could be published back to Nacos")
	}
}
