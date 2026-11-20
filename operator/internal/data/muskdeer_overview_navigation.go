package data

import (
	"context"
	"slices"
	"time"

	"github.com/hmlax/muskdeer/deer/management/constants"
	"github.com/hmlax/muskdeer/deer/management/muskdeer"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/menu"
	"google.golang.org/protobuf/proto"
)

// ensureMuskdeerOverviewNavigation reuses the dashboard identities and metadata.
// Linking the existing overview permission preserves custom role and deny policy.
func ensureMuskdeerOverviewNavigation(ctx context.Context, tx *store.Tx, now time.Time) ([]uint32, error) {
	ids := make([]uint32, 0, 2)
	var parentID *uint32
	for _, seed := range constants.DefaultMenus {
		if seed.GetName() != muskdeer.OverviewDashboardMenuName && seed.GetName() != muskdeer.OverviewAnalyticsMenuName {
			continue
		}
		row, err := tx.Menu.Query().Where(menu.NameEQ(seed.GetName())).Only(ctx)
		if store.IsNotFound(err) {
			row, err = tx.Menu.Create().
				SetName(seed.GetName()).
				SetNillableParentID(parentID).
				SetPath(seed.GetPath()).
				SetComponent(seed.GetComponent()).
				SetModule(pb.Module_DASHBOARD.String()).
				SetType(seed.GetType().String()).
				SetStatus(pb.Menu_ON.String()).
				SetCreatedAt(now).
				SetUpdatedAt(now).
				SetMeta(proto.Clone(seed.GetMeta()).(*pb.MenuMeta)).
				Save(ctx)
		}
		if err != nil {
			return nil, err
		}
		meta := row.Meta
		if meta == nil {
			meta = &pb.MenuMeta{}
		} else {
			meta = proto.Clone(meta).(*pb.MenuMeta)
		}
		if !slices.Contains(meta.Authority, muskdeer.OverviewPermissionCode) {
			meta.Authority = append(meta.Authority, muskdeer.OverviewPermissionCode)
			if _, err := tx.Menu.UpdateOneID(row.ID).SetMeta(meta).SetUpdatedAt(now).Save(ctx); err != nil {
				return nil, err
			}
		}
		ids = append(ids, row.ID)
		parentID = &row.ID
	}
	return ids, nil
}
