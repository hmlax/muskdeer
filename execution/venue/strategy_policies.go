package venue

import (
	"context"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// InitializeStrategies atomically maintains reviewed fund policies, immutable
// strategy versions and enablement. It never calls the SDK or changes money.
func (s *Store) InitializeStrategies(ctx context.Context, input *commonv1.VenueStrategyImport) error {
	if !validStoreContext(ctx) || len(input.GetStrategies()) == 0 {
		return ErrInvalidSubmit
	}
	writes := proto.CloneOf(input)
	return s.db.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		store, err := NewStore(tx.Session(&gorm.Session{NewDB: true}), s.gateway)
		if err != nil {
			return err
		}
		profiles := &commonv1.VenueProfileImport{}
		for _, write := range writes.Strategies {
			profiles.Profiles = append(profiles.Profiles, write.GetProfile())
		}
		if err := store.ImportProfiles(ctx, profiles); err != nil {
			return err
		}
		if err := venueconfig.ImportStrategies(ctx, tx, writes); err != nil {
			return err
		}
		for _, profile := range profiles.Profiles {
			if err := store.InitializeBudget(ctx, profile, true); err != nil {
				return err
			}
		}
		return nil
	})
}
