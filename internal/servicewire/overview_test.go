package servicewire

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	adminv1 "github.com/hmlax/muskdeer/api/admin/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func serviceWireFixture() domain.AdminServiceOverview {
	checked := time.UnixMilli(9007199254740994)
	return domain.AdminServiceOverview{
		Role:                 domain.AdminServiceStrategy,
		Source:               domain.AdminServiceRegistry,
		MembershipObservedAt: time.UnixMilli(9007199254740993),
		Total:                2,
		ProbeConfigured:      true,
		Items: []domain.AdminServiceInstance{{
			InstanceID:      "node-a",
			DeclaredVersion: "declared-example",
			Status:          domain.AdminServiceProbeFailed,
			ProbedAt:        &checked,
		}},
		NextAfterInstanceID: "node-a",
	}
}

func TestServiceWireKeepsSampleTimesAndMissingProbeSeparate(t *testing.T) {
	value := serviceWireFixture()
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceStrategy,
		Limit: 1,
	}
	reply, err := OverviewProto(value, filter)
	if err != nil || reply.MembershipObservedAtMs != 9007199254740993 || reply.Items[0].ProbedAtMs == nil || *reply.Items[0].ProbedAtMs != 9007199254740994 ||
		reply.Items[0].Status != string(domain.AdminServiceProbeFailed) || reply.Items[0].DeclaredVersion != "declared-example" {
		t.Fatal("wire rounded observation or changed diagnostics", err)
	}
	encoded, err := proto.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	var decoded adminv1.ServiceOverview
	if err := proto.Unmarshal(encoded, &decoded); err != nil || !proto.Equal(reply, &decoded) {
		t.Fatal("service observation lost typed protobuf roundtrip", err)
	}
	*reply.Items[0].ProbedAtMs = 1
	if value.Items[0].ProbedAt.UnixMilli() != 9007199254740994 {
		t.Fatal("wire exposed a shared timestamp pointer")
	}
	value.ProbeConfigured = false
	value.Items[0].Status, value.Items[0].ProbedAt = domain.AdminServiceProbeNotConfigured, nil
	if reply, err := OverviewProto(value, filter); err != nil || reply.Items[0].ProbedAtMs != nil || reply.ProbeConfigured {
		t.Fatal("unperformed check gained a timestamp", err)
	}
}

func TestServiceWireRejectsMalformedScopeCursorStatusAndTime(t *testing.T) {
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceStrategy,
		Limit: 1,
	}
	for i, change := range []func(*domain.AdminServiceOverview){
		func(v *domain.AdminServiceOverview) { v.Role = domain.AdminServiceMarket },
		func(v *domain.AdminServiceOverview) { v.Source = domain.AdminServiceLocalAPI },
		func(v *domain.AdminServiceOverview) { v.Total = 0 },
		func(v *domain.AdminServiceOverview) { v.Total = domain.MaxAdminServiceInstances + 1 },
		func(v *domain.AdminServiceOverview) { v.Items = nil },
		func(v *domain.AdminServiceOverview) { v.Items[0].InstanceID = "" },
		func(v *domain.AdminServiceOverview) { v.Items[0].InstanceID = "127.0.0.1:80" },
		func(v *domain.AdminServiceOverview) { v.Items[0].DeclaredVersion = "bad\nversion" },
		func(v *domain.AdminServiceOverview) { v.Items[0].Status = "NEW_UNKNOWN_STATUS" },
		func(v *domain.AdminServiceOverview) { v.ProbeConfigured = false },
		func(v *domain.AdminServiceOverview) { v.Items[0].ProbedAt = nil },
		func(v *domain.AdminServiceOverview) { v.NextAfterInstanceID = "other" },
		func(v *domain.AdminServiceOverview) { v.NextAfterInstanceID = "" },
		func(v *domain.AdminServiceOverview) { v.Items = append(v.Items, v.Items[0]) },
		func(v *domain.AdminServiceOverview) {
			v.MembershipObservedAt = v.MembershipObservedAt.Add(time.Nanosecond)
		},
		func(v *domain.AdminServiceOverview) { v.MembershipObservedAt = time.Time{} },
		func(v *domain.AdminServiceOverview) { v.Items[0].ProbedAt = new(time.Time) },
		func(v *domain.AdminServiceOverview) {
			checked := v.Items[0].ProbedAt.Add(time.Nanosecond)
			v.Items[0].ProbedAt = &checked
		},
	} {
		value := serviceWireFixture()
		change(&value)
		if reply, err := OverviewProto(value, filter); err == nil || reply != nil {
			t.Fatal("malformed domain success gained partial wire data", i)
		}
	}
}

func TestServiceWireAllowsLocalAPIAndExhaustedPagesWithoutInventingClusterSize(t *testing.T) {
	value := serviceWireFixture()
	value.Role, value.Source, value.Total = domain.AdminServiceAPI, domain.AdminServiceLocalAPI, 1
	value.Items[0].Status = domain.AdminServiceNotReady
	value.NextAfterInstanceID = ""
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceAPI,
		Limit: 1,
	}
	if reply, err := OverviewProto(value, filter); err != nil || reply.Total != 1 || reply.MembershipSource != string(domain.AdminServiceLocalAPI) {
		t.Fatal("local API was misclassified as all API replicas", err)
	}
	value.Items[0].Status = domain.AdminServiceProbeDenied
	if reply, err := OverviewProto(value, filter); err == nil || reply != nil {
		t.Fatal("local CheckReady acquired a network-only result")
	}
	value.Items = nil
	filter.AfterInstanceID = "node-z"
	if reply, err := OverviewProto(value, filter); err != nil || reply.Total != 1 || len(reply.Items) != 0 {
		t.Fatal("exhausted cursor erased existing local API", err)
	}
	value = serviceWireFixture()
	value.Items, value.NextAfterInstanceID = nil, ""
	filter.Role = domain.AdminServiceStrategy
	if reply, err := OverviewProto(value, filter); err != nil || reply.Total != 2 || len(reply.Items) != 0 {
		t.Fatal("exhausted registry page erased other members", err)
	}
}
