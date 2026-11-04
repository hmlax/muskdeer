package runtimeconfig

import (
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	configv1 "github.com/hmlax/muskdeer/deer/proto/common/config/v1"
)

func TestNativeNacosRejectsBusinessProfiles(t *testing.T) {
	var cfg configv1.HimalayaRuntime
	if err := protojson.Unmarshal([]byte(`{"profiles":[{"user_id":101,"fund_id":201,"symbol":"BTCUSDT"}]}`), &cfg); err == nil {
		t.Fatal("Nacos still accepts account/product business profiles instead of requiring database records")
	}
}
