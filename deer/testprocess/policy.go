package testprocess

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	policymigrate "github.com/hmlax/muskdeer/deer/migrate/runtimepolicy"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	"github.com/hmlax/muskdeer/deer/postgres"
)

const (
	messageIsolatedPolicyFixture = "process policy fixture requires the isolated test database"
	messagePolicyFixtureConflict = "process fixture cannot replace an existing database policy"
)

// prepareFixturePolicy provisions only initial test inputs. Production binaries
// never import testprocess and only read database policies. Existing rows cannot
// be reconfigured here; changing parameters requires the explicit maintenance API.
func prepareFixturePolicy(ctx context.Context, cfg *configv1.ServiceConfig) (result error) {
	if cfg.Name == "cron" || cfg.Postgres == nil || managementFixture(cfg) {
		return nil
	}
	if os.Getenv("MUSKDEER_TEST_POSTGRES_DATABASE") == "" || cfg.Postgres.Database != os.Getenv("MUSKDEER_TEST_POSTGRES_DATABASE") {
		return errors.New(messageIsolatedPolicyFixture)
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client, err := postgres.New(bounded, cfg.Postgres, "process-policy-fixture")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, client.Close()) }()
	db := client.GORM()
	if err := policymigrate.Apply(bounded, db); err != nil {
		return err
	}
	var existing model.ServiceRuntimePolicy
	err = db.WithContext(bounded).Model(&model.ServiceRuntimePolicy{}).Where("service_name = ?", cfg.Name).Take(&existing).Error
	if err == nil {
		if cfg.Business != nil && !proto.Equal(existing.Parameters, cfg.Business) {
			return errors.New(messagePolicyFixtureConflict)
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	parameters := proto.CloneOf(cfg.Business)
	if parameters == nil {
		parameters = &configv1.BusinessRuntime{}
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(parameters)
	if err != nil {
		return err
	}
	created := db.WithContext(bounded).Model(&model.ServiceRuntimePolicy{}).Clauses(clause.OnConflict{DoNothing: true}).Create(&model.ServiceRuntimePolicy{
		ServiceName:             cfg.Name,
		Revision:                1,
		ParametersSchemaVersion: 1,
		Parameters:              parameters,
		ParametersDigest:        fmt.Sprintf("%x", sha256.Sum256(encoded)),
	})
	if created.Error != nil {
		return created.Error
	}
	if created.RowsAffected == 0 {
		if err := db.WithContext(bounded).Model(&model.ServiceRuntimePolicy{}).Where("service_name = ?", cfg.Name).Take(&existing).Error; err != nil {
			return err
		}
		if !proto.Equal(existing.Parameters, parameters) {
			return errors.New(messagePolicyFixtureConflict)
		}
	}
	return nil
}

func managementFixture(cfg *configv1.ServiceConfig) bool {
	return cfg.GetBusiness().GetStrategy().GetManagementEnabled() || cfg.GetBusiness().GetStrategy().GetControlEnabled()
}
