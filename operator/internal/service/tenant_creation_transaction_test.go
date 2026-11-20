package service

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/authorizer"
	"github.com/tx7do/go-utils/password"
	"github.com/tx7do/go-utils/trans"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	"github.com/hmlax/muskdeer/operator/internal/data"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	modelRole "github.com/hmlax/muskdeer/operator/internal/data/store/role"
	storetest "github.com/hmlax/muskdeer/operator/internal/data/storetest"

	"github.com/hmlax/muskdeer/deer/management/constants"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

// The participant writes through the service's transaction, so credential
// validation failures must roll back these rows along with the tenant/role.
type transactionalTenantUsers struct{ data.UserRepo }

func (transactionalTenantUsers) CreateWithTx(ctx context.Context, tx *store.Tx, user *operatorV1.User) (*operatorV1.User, error) {
	row, err := tx.User.Create().SetTenantID(user.GetTenantId()).SetUsername(user.GetUsername()).Save(ctx)
	if err != nil {
		return nil, err
	}
	if err := tx.UserRole.Create().SetTenantID(user.GetTenantId()).SetUserID(row.ID).SetRoleID(user.GetRoleId()).Exec(ctx); err != nil {
		return nil, err
	}
	return &operatorV1.User{
		Id:       trans.Ptr(row.ID),
		TenantId: user.TenantId,
		Username: user.Username,
	}, nil
}

func TestTenantCreationTransaction(t *testing.T) {
	for _, test := range []struct {
		name     string
		password string
		success  bool
	}{
		{
			name:     "weak password rolls back all participants",
			password: "123",
		},
		{
			name:     "valid password commits all participants",
			password: constants.DefaultUserPassword,
			success:  true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := storetest.NewClientForTest(t)
			ctx := auth.NewContext(storetest.NewSystemContext(t.Context()), &operatorV1.UserTokenPayload{UserId: 1})
			_, err := client.Client().Role.Create().SetTenantID(0).SetName("template").
				SetCode(constants.TenantAdminTemplateRoleCode).SetType(modelRole.TypeTemplate).
				SetStatus(modelRole.StatusOn).SetIsProtected(true).Save(ctx)
			require.NoError(t, err)
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { require.NoError(t, rdb.Close()) })
			svc := &TenantService{
				log:                 bLogger.NewHelper(bLogger.NopLogger()),
				tenantRepo:          data.NewTenantRepoForTest(client),
				roleRepo:            data.NewRoleRepoForTest(client),
				userRepo:            transactionalTenantUsers{},
				userCredentialsRepo: data.NewUserCredentialRepoForTest(client, password.NewBCryptCrypto(), data.NewConfigRepoForTest(client, rdb)),
				authorizer:          authorizer.NewAuthorizer(ctx, bLogger.NopLogger(), &authorizer.EngineConfig{Type: "noop"}, stubAuthzProvider{}),
			}
			_, err = svc.CreateTenantWithAdminUser(ctx, &operatorV1.CreateTenantWithAdminUserRequest{
				Tenant: &operatorV1.Tenant{
					Name: trans.Ptr("transaction-test"),
					Code: trans.Ptr("transaction-test"),
				},
				User:     &operatorV1.User{Username: trans.Ptr("owner")},
				Password: test.password,
			})
			if test.success {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			want := 0
			if test.success {
				want = 1
			}
			for name, count := range map[string]func(context.Context) (int, error){
				"tenants":     client.Client().Tenant.Query().Count,
				"users":       client.Client().User.Query().Count,
				"credentials": client.Client().UserCredential.Query().Count,
				"user roles":  client.Client().UserRole.Query().Count,
				"metadata":    client.Client().RoleMetadata.Query().Count,
			} {
				got, countErr := count(ctx)
				require.NoError(t, countErr)
				require.Equal(t, want, got, "%s must commit or roll back with the tenant", name)
			}
			roles, err := client.Client().Role.Query().Count(ctx)
			require.NoError(t, err)
			require.Equal(t, 1+want, roles, "the platform template must survive rollback")
		})
	}
}
