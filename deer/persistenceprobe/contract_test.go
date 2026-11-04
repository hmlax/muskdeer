package persistenceprobe

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm/schema"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
)

const precisePriceTick = "12345678901234567890.123456789012345678"

func TestGeneratedModelSchema(t *testing.T) {
	t.Parallel()
	metadata, err := schema.Parse(&commonmodelv1.Instrument{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("schema.Parse(Instrument) = %v, want a valid generated model", err)
	}
	if len(metadata.DBNames) != 13 {
		t.Errorf("schema.Parse(Instrument) columns = %v, want exactly 13 business fields", metadata.DBNames)
	}
	id := metadata.FieldsByName["Id"]
	if id == nil || !id.PrimaryKey || !id.AutoIncrement {
		t.Fatal("Instrument.Id must be an auto-increment primary key")
	}
	for _, name := range []string{"CreateAt", "UpdateAt"} {
		field := metadata.FieldsByName[name]
		if field == nil {
			t.Fatalf("Instrument.%s is missing from the generated model", name)
		}
		if !field.NotNull {
			t.Errorf("Instrument.%s must be NOT NULL", name)
		}
	}
	if metadata.FieldsByName["CreateAt"].AutoCreateTime != schema.UnixMillisecond {
		t.Error("Instrument.CreateAt must use automatic UTC Unix milliseconds")
	}
	if metadata.FieldsByName["UpdateAt"].AutoUpdateTime != schema.UnixMillisecond {
		t.Error("Instrument.UpdateAt must use automatic UTC Unix milliseconds")
	}
	deleted := metadata.FieldsByName["DeleteAt"]
	if deleted == nil || deleted.NotNull || deleted.FieldType.Kind() != reflect.Pointer {
		t.Fatal("Instrument.DeleteAt must preserve SQL NULL using a pointer")
	}
	price := metadata.FieldsByName["PriceTick"]
	if price == nil || price.FieldType.Kind() != reflect.String || price.DataType != "numeric(38,18)" {
		t.Fatal("Instrument.PriceTick must be a generated string backed by numeric(38,18)")
	}
}

func TestGeneratedProtoDecimalAndNullRoundTrip(t *testing.T) {
	t.Parallel()
	for _, deleted := range []*int64{nil, proto.Int64(0), proto.Int64(1792930000123)} {
		original := &commonmodelv1.Instrument{
			PriceTick: precisePriceTick,
			DeleteAt:  deleted,
		}
		encoded, err := proto.Marshal(original)
		if err != nil {
			t.Fatalf("proto.Marshal(Instrument) = %v", err)
		}
		var decoded commonmodelv1.Instrument
		if err := proto.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("proto.Unmarshal(Instrument) = %v", err)
		}
		if !proto.Equal(original, &decoded) {
			t.Errorf("protobuf round trip = %v, want %v", &decoded, original)
		}
		wire, err := protojson.Marshal(&decoded)
		if err != nil {
			t.Fatalf("protojson.Marshal(Instrument) = %v", err)
		}
		var output struct {
			PriceTick string `json:"priceTick"`
		}
		if err := json.Unmarshal(wire, &output); err != nil {
			t.Fatalf("JSON decimal string decode = %v", err)
		}
		if output.PriceTick != precisePriceTick {
			t.Errorf("JSON price = %q, want %q", output.PriceTick, precisePriceTick)
		}
		value, err := decimal.NewFromString(decoded.PriceTick)
		if err != nil || value.String() != precisePriceTick {
			t.Errorf("decimal parse(%q) = %v, %v", decoded.PriceTick, value, err)
		}
	}
	var invalid commonmodelv1.Instrument
	if err := protojson.Unmarshal([]byte(`{"priceTick":0.1}`), &invalid); err == nil {
		t.Error("protojson.Unmarshal(number into price) succeeded, want string type rejection")
	}
}

func TestDecimalAdditionIsExact(t *testing.T) {
	t.Parallel()
	left, err := decimal.NewFromString("0.1")
	if err != nil {
		t.Fatal(err)
	}
	right, err := decimal.NewFromString("0.2")
	if err != nil {
		t.Fatal(err)
	}
	if got := left.Add(right).String(); got != "0.3" {
		t.Errorf("decimal.Add(0.1, 0.2) = %s, want 0.3", got)
	}
}
