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

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

const precisePrice = "12345678901234567890.123456789012345678"

func TestGeneratedModelSchema(t *testing.T) {
	t.Parallel()
	metadata, err := schema.Parse(&modelv1.StorageProbe{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("schema.Parse(StorageProbe) = %v, want a valid generated model", err)
	}
	if len(metadata.DBNames) != 9 {
		t.Errorf("schema.Parse(StorageProbe) columns = %v, want exactly 9 business fields", metadata.DBNames)
	}
	id := metadata.FieldsByName["Id"]
	if id == nil || !id.PrimaryKey || !id.AutoIncrement {
		t.Fatal("StorageProbe.Id must be an auto-increment primary key")
	}
	for _, name := range []string{"CreateAt", "UpdateAt"} {
		field := metadata.FieldsByName[name]
		if field == nil {
			t.Fatalf("StorageProbe.%s is missing from the generated model", name)
		}
		if !field.NotNull {
			t.Errorf("StorageProbe.%s must be NOT NULL", name)
		}
	}
	if metadata.FieldsByName["CreateAt"].AutoCreateTime != schema.UnixMillisecond {
		t.Error("StorageProbe.CreateAt must use automatic UTC Unix milliseconds")
	}
	if metadata.FieldsByName["UpdateAt"].AutoUpdateTime != schema.UnixMillisecond {
		t.Error("StorageProbe.UpdateAt must use automatic UTC Unix milliseconds")
	}
	deleted := metadata.FieldsByName["DeleteAt"]
	if deleted == nil || deleted.NotNull || deleted.FieldType.Kind() != reflect.Pointer {
		t.Fatal("StorageProbe.DeleteAt must preserve SQL NULL using a pointer")
	}
	price := metadata.FieldsByName["Price"]
	if price == nil || price.FieldType.Kind() != reflect.String || price.DataType != "numeric(38,18)" {
		t.Fatal("StorageProbe.Price must be a generated string backed by numeric(38,18)")
	}
}

func TestGeneratedProtoDecimalAndNullRoundTrip(t *testing.T) {
	t.Parallel()
	for _, deleted := range []*int64{nil, proto.Int64(0), proto.Int64(1792930000123)} {
		original := &modelv1.StorageProbe{
			Price:    precisePrice,
			DeleteAt: deleted,
		}
		encoded, err := proto.Marshal(original)
		if err != nil {
			t.Fatalf("proto.Marshal(StorageProbe) = %v", err)
		}
		var decoded modelv1.StorageProbe
		if err := proto.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("proto.Unmarshal(StorageProbe) = %v", err)
		}
		if !proto.Equal(original, &decoded) {
			t.Errorf("protobuf round trip = %v, want %v", &decoded, original)
		}
		wire, err := protojson.Marshal(&decoded)
		if err != nil {
			t.Fatalf("protojson.Marshal(StorageProbe) = %v", err)
		}
		var output struct {
			Price string `json:"price"`
		}
		if err := json.Unmarshal(wire, &output); err != nil {
			t.Fatalf("JSON decimal string decode = %v", err)
		}
		if output.Price != precisePrice {
			t.Errorf("JSON price = %q, want %q", output.Price, precisePrice)
		}
		value, err := decimal.NewFromString(decoded.Price)
		if err != nil || value.String() != precisePrice {
			t.Errorf("decimal parse(%q) = %v, %v", decoded.Price, value, err)
		}
	}
	var invalid modelv1.StorageProbe
	if err := protojson.Unmarshal([]byte(`{"price":0.1}`), &invalid); err == nil {
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
