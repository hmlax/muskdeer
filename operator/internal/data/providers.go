package data

import (
	"github.com/google/wire"
	"github.com/tx7do/go-utils/authorizer"
	"github.com/tx7do/go-utils/geoip/geolite"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
)

// ProviderSet supplies infrastructure and GORM repositories to the operator.
var ProviderSet = wire.NewSet(
	NewRedisClient,
	NewMuskdeerConnection,
	NewMuskdeerBindingRepo,
	NewGormClient,
	NewMinIoClient,
	NewClientType,
	NewPasswordCrypto,
	NewCaptcha,
	NewUserTokenCache,
	NewAuthenticator,
	NewTokenChecker,
	NewLoginRateLimiter,
	NewMfaChallengeCache,
	NewSsoStateCache,
	NewVCodeCache,
	NewUserRoleRepo,
	NewUserOrgUnitRepo,
	NewUserPositionRepo,
	NewMembershipRoleRepo,
	NewMembershipPositionRepo,
	NewMembershipOrgUnitRepo,
	NewMembershipRepo,
	NewUserRepo,
	NewConfigRepo,
	NewUserCredentialRepo,
	NewUserMfaFactorRepo,
	NewLoginPolicyRepo,
	NewOrgUnitRepo,
	NewPositionRepo,
	NewTenantRepo,

	NewPermissionApiRepo,
	NewPermissionMenuRepo,
	NewRolePermissionRepo,
	NewRoleOrgUnitRepo,
	NewRoleFieldPermissionRepo,
	NewRoleMetadataRepo,
	NewPermissionRepo,
	NewRoleRepo,
	NewPermissionGroupRepo,
	NewApiRepo,
	NewMenuRepo,
	NewApiAuditLogRepo,
	NewLoginAuditLogRepo,
	NewOperationAuditLogRepo,
	NewPermissionAuditLogRepo,
	NewDataAccessAuditLogRepo,
	NewPolicyEvaluationLogRepo,

	NewDictTypeRepo,
	NewDictEntryI18nRepo,
	NewDictEntryRepo,
	NewLanguageRepo,

	NewFileRepo,

	NewAccessKeyRepo,

	NewGatewayAccessRepo,
	NewAuthorizerProvider,

	NewGeoClient,
	NewAuthorizer,
	// register:repo
)

// NewGeoClient owns the GeoIP reader for the application lifetime.
func NewGeoClient(ctx *bootstrap.Context) (*geolite.Client, func(), error) {
	client, err := geolite.NewClient()
	if err != nil {
		return nil, nil, err
	}
	log := ctx.NewLoggerHelper("geoip/data/operator")
	return client, func() {
		if err := client.Close(); err != nil {
			log.Errorf(ctx.Context(), "close GeoIP reader: %v", err)
		}
	}, nil
}

// NewAuthorizer uses the configured policy engine and repository provider.
func NewAuthorizer(ctx *bootstrap.Context, provider authorizer.Provider) *authorizer.Authorizer {
	var config *authorizer.EngineConfig
	if bootstrapConfig := ctx.GetConfig(); bootstrapConfig != nil && bootstrapConfig.Authz != nil {
		config = &authorizer.EngineConfig{Type: bootstrapConfig.Authz.GetType()}
	}
	return authorizer.NewAuthorizer(ctx.Context(), ctx.GetLogger(), config, provider)
}
