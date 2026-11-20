//go:build integration

package store

import (
	"net/url"
	"os"
	"testing"

	"github.com/hmlax/muskdeer/deer/migrate/adminschema"

	"github.com/tx7do/go-utils/trans"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/hmlax/muskdeer/deer/management/constants"
)

// TestServiceSmokeSchema prepares only the throwaway service-compose fixture.
// Runtime startup deliberately performs no schema changes.
func TestServiceSmokeSchema(t *testing.T) {
	dsn := os.Getenv("SERVICE_SMOKE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SERVICE_SMOKE_POSTGRES_DSN for isolated service smoke setup")
	}
	target, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if target.Hostname() != "127.0.0.1" || target.Port() == "" || target.Port() == "5432" || target.Path != "/gwa" {
		t.Fatal("schema fixture requires a loopback ephemeral port and the gwa test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Errorf("close schema fixture: %v", err)
		}
	})
	if err := NewClient(db).Schema.Create(t.Context()); err != nil {
		t.Fatal(err)
	}
	// The fixture needs the standard roles before bootstrapping users. Keep its
	// authorization facts explicit instead of bypassing the real login checks.
	for index, defaults := range constants.DefaultRoles {
		role := &Role{
			ID:          uint32(index + 1),
			TenantID:    trans.Ptr(constants.PlatformTenantID),
			Name:        defaults.Name,
			Code:        defaults.Code,
			Status:      trans.Ptr(defaults.GetStatus().String()),
			IsProtected: defaults.IsProtected,
			Type:        trans.Ptr(defaults.GetType().String()),
			Description: defaults.Description,
			SortOrder:   defaults.SortOrder,
		}
		if err := db.WithContext(t.Context()).Clauses(clause.OnConflict{DoNothing: true}).Create(role).Error; err != nil {
			t.Fatal(err)
		}
		for _, permissionID := range defaults.GetPermissions() {
			link := &RolePermission{
				TenantID:     trans.Ptr(constants.PlatformTenantID),
				RoleID:       trans.Ptr(role.ID),
				PermissionID: trans.Ptr(permissionID),
			}
			if err := db.WithContext(t.Context()).Clauses(clause.OnConflict{DoNothing: true}).Create(link).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	// Explicit fixture IDs do not advance a PostgreSQL sequence. New tenant roles
	// must receive IDs after the seeded system roles, just as on a migrated DB.
	if err := adminschema.ResetSequence(t.Context(), db, &Role{}, len(constants.DefaultRoles)); err != nil {
		t.Fatal(err)
	}
}
