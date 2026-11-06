package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
)

func TestReadProfilesLoadsEveryConfiguredMarket(t *testing.T) {
	t.Parallel()
	for _, count := range []int{19, 33, 257} {
		t.Run(fmt.Sprintf("%d markets", count), func(t *testing.T) {
			t.Parallel()
			input := &commonv1.VenueProfileImport{}
			for i := range count {
				input.Profiles = append(input.Profiles, &commonv1.VenueProfilePolicy{
					UserId: int64(i + 1),
					FundId: int64(i + 1),
					Symbol: fmt.Sprintf("COIN%dUSDT", i),
				})
			}
			data, err := protojson.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "profiles.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := readProfiles(path)
			if err != nil || len(got.GetProfiles()) != count {
				t.Fatalf("readProfiles(%d markets) = %d profiles, %v, want complete import", count, len(got.GetProfiles()), err)
			}

		})
	}
}
