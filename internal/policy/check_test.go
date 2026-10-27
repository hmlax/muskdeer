package policy

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/srikrsna/protoc-gen-gotag/tagger"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestGoSyntaxPolicyIgnoresStringsAndMetadataButRejectsTableAndFloats(t *testing.T) {
	for _, test := range []struct {
		source string
		bad    bool
	}{
		{
			source: `package p; func f(db X) { (db.Table)("orders") }`,
			bad:    true,
		},
		{
			source: `package p; var rate = 1e-3`,
			bad:    true,
		},
		{
			source: `package p; func (row X) TableName() string { return "orders" }`,
			bad:    true,
		},
		{
			source: `package p; var text = "db.Table(orders) and 0.1"; func f(schema X) { _ = schema.Table }`,
		},
		{
			source: `package p; func f(db X) { db.WithContext(ctx).Model(&Record{}).First(&row) }`,
		},
	} {
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, "fixture.go", test.source, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		if issues := GoSource(fileSet, file); (len(issues) != 0) != test.bad {
			t.Fatalf("source policy = %v, want bad=%v", issues, test.bad)
		}
	}
}

func TestProtoPolicyRejectsFloatingWireFieldsAndInvalidModelTags(t *testing.T) {
	field := func(name string, kind descriptorpb.FieldDescriptorProto_Type, tags string) *descriptorpb.FieldDescriptorProto {
		options := &descriptorpb.FieldOptions{}
		proto.SetExtension(options, tagger.E_Tags, tags)
		return &descriptorpb.FieldDescriptorProto{
			Name:    proto.String(name),
			Type:    kind.Enum(),
			Options: options,
		}
	}
	model := &descriptorpb.DescriptorProto{
		Name: proto.String("Entity"),
		Field: []*descriptorpb.FieldDescriptorProto{
			field("id", descriptorpb.FieldDescriptorProto_TYPE_INT64, `gorm:"column:id;type:bigint;primaryKey;autoIncrement"`),
			field("create_at", descriptorpb.FieldDescriptorProto_TYPE_INT64, `gorm:"column:create_at;type:bigint;not null;autoCreateTime:milli"`),
			field("update_at", descriptorpb.FieldDescriptorProto_TYPE_INT64, `gorm:"column:update_at;type:bigint;not null;autoUpdateTime:milli"`),
			field("delete_at", descriptorpb.FieldDescriptorProto_TYPE_INT64, `gorm:"column:delete_at;type:bigint"`),
			field("price", descriptorpb.FieldDescriptorProto_TYPE_STRING, `gorm:"column:price;type:numeric(38,18)"`),
		},
	}
	model.Field[3].Proto3Optional = proto.Bool(true)
	set := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{
				Name:        proto.String("deer/proto/model/v1/entity.proto"),
				MessageType: []*descriptorpb.DescriptorProto{model},
			},
		},
	}
	if issues := Descriptors(set); len(issues) != 0 {
		t.Fatalf("valid generated-model contract failed: %v", issues)
	}
	for _, change := range []func(*descriptorpb.DescriptorProto){
		func(model *descriptorpb.DescriptorProto) { model.Field[0].Options = nil },
		func(model *descriptorpb.DescriptorProto) { model.Field[1].Options = nil },
		func(model *descriptorpb.DescriptorProto) { model.Field[2].Options = nil },
		func(model *descriptorpb.DescriptorProto) { model.Field[3].Proto3Optional = nil },
		func(model *descriptorpb.DescriptorProto) {
			model.Field[4].Type = descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum()
		},
		func(model *descriptorpb.DescriptorProto) {
			model.Field[4].Type = descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum()
		},
		func(model *descriptorpb.DescriptorProto) { model.Field = model.Field[1:] },
	} {
		changed := proto.CloneOf(set)
		change(changed.File[0].MessageType[0])
		if issues := Descriptors(changed); len(issues) == 0 {
			t.Fatal("invalid model contract passed policy")
		}
	}
	wire := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{
				Name: proto.String("api/example/v1/contract.proto"),
				MessageType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("Reply"),
						NestedType: []*descriptorpb.DescriptorProto{
							{
								Name: proto.String("Value"),
								Field: []*descriptorpb.FieldDescriptorProto{
									field("quantity", descriptorpb.FieldDescriptorProto_TYPE_FLOAT, ""),
								},
							},
						},
					},
				},
			},
		},
	}
	if issues := Descriptors(wire); len(issues) != 1 {
		t.Fatalf("nested wire float was not rejected: %v", issues)
	}
}
