package domain

import "testing"

func TestRiskSnapshotEventKindsKeepRecordingStartDistinctFromChanges(t *testing.T) {
	for _, kind := range []RiskSnapshotEventKind{RiskSnapshotEventBaseline, RiskSnapshotEventChanged, RiskSnapshotEventRemoved} {
		if !ValidRiskSnapshotEventKind(kind) {
			t.Fatal("known risk diagnostic recording kind rejected")
		}
	}
	for _, kind := range []RiskSnapshotEventKind{"", "baseline", "ALLOW", "RECONCILED"} {
		if ValidRiskSnapshotEventKind(kind) {
			t.Fatal("undefined kind or risk permission masqueraded as a recording kind")
		}
	}
}
