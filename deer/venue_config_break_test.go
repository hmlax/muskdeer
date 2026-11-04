package deer

import (
	"testing"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestVenueConfigurationRejectsRetiredProviderKey(t *testing.T) {
	var business configv1.BusinessRuntime
	if err := protojson.Unmarshal([]byte(`{"himalaya":{"depth":1}}`), &business); err == nil {
		t.Fatal("retired himalaya JSON configuration key was accepted")
	}
	data := "name: strategy\nversion: test\nbusiness:\n  himalaya: {depth: 1}\n"
	if _, err := decodeConfigPair(t.Context(), []byte(data), []byte(yamlService(data))); err == nil {
		t.Fatal("retired himalaya YAML configuration key was accepted")
	}
}
