package venuemigrate

import (
	"strings"
	"testing"

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
)

func TestKnownMigrationPathsNeverReplayHistoricalDrops(t *testing.T) {
	t.Parallel()
	canonical := definitions()
	legacy := historyRows(canonical, 6)
	legacy = append(legacy, &commonmodelv1.VenueSchemaMigration{
		Version:  7,
		Checksum: checksum(legacyStatelessDefinition),
	})
	stateless := definitionsFor(legacy)
	for _, test := range []struct {
		name    string
		bodies  []string
		applied int
		pending int
	}{
		{
			name:    "fresh database",
			bodies:  canonical,
			pending: 9,
		},
		{
			name:    "retained version six",
			bodies:  canonical,
			applied: 6,
			pending: 3,
		},
		{
			name:    "historical stateless version seven",
			bodies:  stateless,
			applied: 7,
			pending: 4,
		},
		{
			name:    "historical stateless version eight",
			bodies:  stateless,
			applied: 8,
			pending: 3,
		},
		{
			name:    "upgraded stateless version ten",
			bodies:  stateless,
			applied: 10,
			pending: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rows := historyRows(test.bodies, test.applied)
			if err := validate(rows, false); err != nil {
				t.Fatalf("validate(%s) = %v, want recognized exact history", test.name, err)
			}
			path := definitionsFor(rows)
			if len(path)-len(rows) != test.pending {
				t.Fatalf("pending migrations(%s) = %d, want %d", test.name, len(path)-len(rows), test.pending)
			}
			for _, statement := range path[len(rows):] {
				if strings.Contains(statement, "DROP TABLE") {
					t.Fatalf("pending migrations(%s) include a historical table deletion", test.name)
				}
			}
		})
	}
	legacy[6].Checksum = strings.Repeat("f", 64)
	if err := validate(legacy, false); err == nil {
		t.Fatal("validate(unknown version seven checksum) accepted an undocumented history")
	}
}

func historyRows(bodies []string, count int) []*commonmodelv1.VenueSchemaMigration {
	rows := make([]*commonmodelv1.VenueSchemaMigration, 0, count)
	for i := range count {
		rows = append(rows, &commonmodelv1.VenueSchemaMigration{
			Version:  int64(i + 1),
			Checksum: checksum(bodies[i]),
		})
	}
	return rows
}
