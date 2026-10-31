package adminopenapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestEveryPublicResponseCompilesAndAcceptsProtoJSONDefaults(t *testing.T) {
	compiler := documentCompiler(t)
	for _, definition := range definitions() {
		if definition.reply == nil {
			continue
		}
		t.Run(definition.id, func(t *testing.T) {
			name := string(definition.reply.ProtoReflect().Descriptor().FullName())
			schema := compileComponent(t, compiler, name)
			wire, err := (protojson.MarshalOptions{
				UseProtoNames:   true,
				EmitUnpopulated: true,
			}).Marshal(definition.reply)
			if err != nil {
				t.Fatal(err)
			}
			validateJSON(t, schema, wire, true)
		})
	}
}

func TestInputsKeepTypedUnionDecimalsAndRiskPreconditions(t *testing.T) {
	compiler := documentCompiler(t)
	creation := compileComponent(t, compiler, "muskdeer.strategy.v1.CreateStrategyInput.Input")
	grid := `{"instance_key":"schema-fixture","account_id":"schema-account","instrument_id":"9007199254740993","code_version":"schema-v1","market_data_source":"okx","market_mode":"REPLAY","reason":"schema fixture","parameters":{"grid":{"lower_price":"1.000000000000000001","upper_price":"2","grid_count":2,"total_budget":"4","per_order_notional":"2","inventory_limit":"10","max_market_age_ms":"1000"}}}`
	validateJSON(t, creation, []byte(grid), true)
	for _, invalid := range []string{
		strings.Replace(grid, `"lower_price":"1.000000000000000001"`, `"lower_price":1.000000000000000001`, 1),
		strings.Replace(grid, `"instrument_id":"9007199254740993"`, `"instrument_id":9007199254740993`, 1),
		strings.Replace(grid, `"1.000000000000000001"`, `"1.0000000000000000001"`, 1),
		strings.Replace(grid, `"parameters":{"grid":`, `"parameters":{"market_maker":{},"grid":`, 1),
		strings.Replace(grid, `"parameters":{"grid":`, `"session":{"token":"private"},"parameters":{"grid":`, 1),
		strings.Replace(grid, `,"inventory_limit":"10"`, "", 1),
	} {
		validateJSON(t, creation, []byte(invalid), false)
	}
	risk := compileComponent(t, compiler, "RiskInput")
	block := `{"action":"` + string(domain.AdminRiskBlock) + `","expected_revision":"1","reason":"schema fixture"}`
	unblock := strings.Replace(block, string(domain.AdminRiskBlock), string(domain.AdminRiskUnblock), 1)
	validateJSON(t, risk, []byte(block), true)
	validateJSON(t, risk, []byte(unblock), false)
	preconditions := `,"expected_config_version":"2","expected_desired_state":"` + string(domain.StrategyStateRunning) + `"}`
	validateJSON(t, risk, []byte(strings.TrimSuffix(unblock, "}")+preconditions), true)
	validateJSON(t, risk, []byte(strings.TrimSuffix(block, "}")+preconditions), false)
	validateJSON(t, risk, []byte(strings.TrimSuffix(block, "}")+`,"expected_config_version":""}`), false)
}

func TestOptionalObservationAndIntegerBounds(t *testing.T) {
	compiler := documentCompiler(t)
	schema := compileComponent(t, compiler, "muskdeer.strategy.v1.StrategyOperation")
	for _, definition := range definitions() {
		if definition.id != "getOperation" {
			continue
		}
		data, err := (protojson.MarshalOptions{
			UseProtoNames:   true,
			EmitUnpopulated: true,
		}).Marshal(definition.reply)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("completed_at_ms")) || !bytes.Contains(data, []byte(`"observation":null`)) {
			t.Fatalf("unexpected ProtoJSON presence: %s", data)
		}
		validateJSON(t, schema, data, true)
		for _, invalid := range []string{`0`, `null`, `"-0"`, `"01"`, `"+1"`, `"1e2"`, `"9223372036854775808"`, `"-9223372036854775809"`} {
			validateJSON(t, schema, bytes.Replace(data, []byte(`"id":"0"`), []byte(`"id":`+invalid), 1), false)
		}
		for _, valid := range []string{`"9007199254740993"`, `"9223372036854775807"`, `"-9223372036854775808"`} {
			validateJSON(t, schema, bytes.Replace(data, []byte(`"id":"0"`), []byte(`"id":`+valid), 1), true)
		}
	}
}

func TestOfficialSchemaIsPinnedAndLoadingIsOffline(t *testing.T) {
	data, err := os.ReadFile("../../third_party/openapi/schema-2022-10-07.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != "da01ba28852cac0de53893797cb8d1942bc3b05084f526dcc216717dec314ed0" {
		t.Fatal("official OpenAPI schema bytes changed")
	}
	compiler := offlineCompiler()
	if _, err := compiler.Compile("https://unexpected.invalid/schema.json"); err == nil || !strings.Contains(err.Error(), "external schema loading prohibited") {
		t.Fatalf("unexpected schema loader: %v", err)
	}
}
