package security

import (
	"github.com/redis/go-redis/v9"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
)

// NewTokenCache constructs the shared Redis token store.
func NewTokenCache(rdb *redis.Client, logger bLogger.Logger) *UserTokenCache {
	return &UserTokenCache{
		rdb: rdb,
		log: bLogger.NewHelper(logger),
	}
}

// NewAuthenticatorWithConfig constructs a token engine with explicit configuration.
func NewAuthenticatorWithConfig(cfg *conf.Authentication_Jwt, cache *UserTokenCache, logger bLogger.Logger) (*Authenticator, error) {
	authn, err := newAdminAuthenticator(cfg)
	if err != nil {
		return nil, err
	}
	return &Authenticator{
		log:                bLogger.NewHelper(logger),
		AdminAuthenticator: authn,
		jwtCfg:             cfg,
		userTokenCache:     cache,
	}, nil
}
