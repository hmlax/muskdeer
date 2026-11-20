package data

import (
	"context"
	"fmt"
	"strings"

	"github.com/tx7do/go-utils/trans"
	"google.golang.org/protobuf/proto"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/api"
	"github.com/hmlax/muskdeer/operator/internal/data/store/permissionapi"
)

// SyncCatalog preserves API identities so existing permission links cannot drift
// to another route when OpenAPI iteration order or catalog contents change.
func (r *ApiRepo) SyncCatalog(ctx context.Context, catalog []*operatorV1.Api) (err error) {
	tx, err := r.dbClient.Client().Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin API catalog sync: %w", err)
	}
	defer store.MakeTxCleanup(tx, &err)()
	txRepo := *r
	txRepo.dbClient = store.NewDBClient(tx.Client, nil)
	existing, err := tx.Api.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("read API catalog: %w", err)
	}
	byRoute := make(map[string]*store.Api, len(existing))
	for _, row := range existing {
		byRoute[apiRouteIdentity(row.GetMethod(), row.GetPath())] = row
	}
	seen := make(map[string]struct{}, len(catalog))
	for _, item := range catalog {
		identity := apiRouteIdentity(item.GetMethod(), item.GetPath())
		if _, duplicate := seen[identity]; duplicate {
			return fmt.Errorf("duplicate API route: %s", identity)
		}
		seen[identity] = struct{}{}
		dto := proto.Clone(item).(*operatorV1.Api)
		if row, found := byRoute[identity]; found {
			dto.Id = trans.Ptr(row.ID)
			if err := txRepo.Update(ctx, &operatorV1.UpdateApiRequest{
				Id:   row.ID,
				Data: dto,
			}); err != nil {
				return fmt.Errorf("update API catalog route %s: %w", identity, err)
			}
			continue
		}
		dto.Id = nil
		if err := txRepo.Create(ctx, &operatorV1.CreateApiRequest{Data: dto}); err != nil {
			return fmt.Errorf("create API catalog route %s: %w", identity, err)
		}
	}
	obsolete := make([]uint32, 0)
	for _, row := range existing {
		if _, found := seen[apiRouteIdentity(row.GetMethod(), row.GetPath())]; !found {
			obsolete = append(obsolete, row.ID)
		}
	}
	if len(obsolete) == 0 {
		return nil
	}
	if _, err := tx.PermissionApi.Delete().Where(permissionapi.APIIDIn(obsolete...)).Exec(ctx); err != nil {
		return fmt.Errorf("remove obsolete API permission links: %w", err)
	}
	if _, err := tx.Api.Delete().Where(api.IDIn(obsolete...)).Exec(ctx); err != nil {
		return fmt.Errorf("remove obsolete API routes: %w", err)
	}
	return nil
}

func apiRouteIdentity(method, path string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + path
}
