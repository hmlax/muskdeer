package handler

import (
	"testing"

	apiV1 "github.com/hmlax/muskdeer/admin/biz/model/admin/v1"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAPIAndRPCContractsOwnTheirData(t *testing.T) {
	if (&apiV1.User{}).ProtoReflect().Descriptor().FullName() == (&operatorV1.User{}).ProtoReflect().Descriptor().FullName() {
		t.Fatal("API User still shares the RPC protobuf descriptor")
	}
	in := &apiV1.UpdateUserRequest{
		Id: 9,
		Data: &apiV1.User{
			Username:  proto.String("reader"),
			TenantId:  proto.Uint32(7),
			RoleIds:   []uint32{3, 4},
			Status:    apiV1.User_NORMAL.Enum(),
			CreatedAt: &timestamppb.Timestamp{Seconds: 100},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"username", "role_ids"}},
	}
	out := toRPCUpdateUserRequest(in)
	if out.GetId() != 9 || out.GetData().GetTenantId() != 7 || out.GetData().GetStatus() != operatorV1.User_NORMAL {
		t.Fatal("RPC mapping changed identity or business state")
	}
	*in.Data.Username = "changed"
	in.Data.RoleIds[0] = 99
	in.Data.CreatedAt.Seconds = 999
	in.UpdateMask.Paths[0] = "email"
	if out.Data.GetUsername() != "reader" || out.Data.RoleIds[0] != 3 || out.Data.CreatedAt.Seconds != 100 || out.UpdateMask.Paths[0] != "username" {
		t.Fatal("API mutation leaked across the RPC boundary")
	}
	if out.Data.Email != nil {
		t.Fatal("absent optional field became present")
	}
}

func TestContractMappingPreservesOneofsAndEmptyCollections(t *testing.T) {
	in := &apiV1.DeleteUserRequest{
		QueryBy: &apiV1.DeleteUserRequest_Username{Username: ""},
	}
	out := toRPCDeleteUserRequest(in)
	if _, ok := out.QueryBy.(*operatorV1.DeleteUserRequest_Username); !ok {
		t.Fatal("selected zero-valued username oneof was lost")
	}
	file := toAPIDownloadFileResponse(&operatorV1.DownloadFileResponse{
		Content: &operatorV1.DownloadFileResponse_File{File: []byte{}},
	})
	content, ok := file.Content.(*apiV1.DownloadFileResponse_File)
	if !ok || content.File == nil || len(content.File) != 0 {
		t.Fatal("selected empty binary response became absent")
	}
	if toAPIListUserResponse(&operatorV1.ListUserResponse{}).Items != nil {
		t.Fatal("nil list became an allocated list")
	}
	if toAPIListUserResponse(&operatorV1.ListUserResponse{Items: []*operatorV1.User{}}).Items == nil {
		t.Fatal("allocated empty list became nil")
	}
	unknown := apiV1.User_Status(987)
	if toRPCUser(&apiV1.User{Status: &unknown}).GetStatus() != operatorV1.User_Status(987) {
		t.Fatal("forward-compatible unknown enum number changed")
	}
}

func TestContractMappingCopiesMapsAndTypedJSONFilterValues(t *testing.T) {
	org := &apiV1.OrgUnit{Attributes: map[string]string{"region": "east"}}
	mapped := toRPCOrgUnit(org)
	org.Attributes["region"] = "west"
	if mapped.Attributes["region"] != "east" {
		t.Fatal("API and RPC share a mutable business map")
	}
	value := structpb.NewStringValue("reader")
	page := &apiV1.PagingRequest{
		FilteringType: &apiV1.PagingRequest_FilterExpr{FilterExpr: &apiV1.FilterExpr{
			Conditions: []*apiV1.FilterCondition{{
				Field:      "username",
				ValueOneof: &apiV1.FilterCondition_JsonValue{JsonValue: value},
			}},
		}},
	}
	rpcPage := toRPCPagingRequest(page)
	value.Kind = &structpb.Value_StringValue{StringValue: "changed"}
	if rpcPage.GetFilterExpr().GetConditions()[0].GetJsonValue().GetStringValue() != "reader" {
		t.Fatal("typed JSON filtering value was aliased or re-encoded incorrectly")
	}
}
