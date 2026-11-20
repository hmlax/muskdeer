package security

import (
	"context"

	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type TokenChecker struct {
	log *bLogger.Helper

	authenticator *Authenticator
	clientType    operatorV1.ClientType_ClientType
}

func NewTokenChecker(
	ctx *bootstrap.Context,
	authenticator *Authenticator,
	clientType operatorV1.ClientType_ClientType) auth.AccessTokenChecker {
	return &TokenChecker{
		log:           bLogger.NewHelper(ctx.GetLogger().With("module", "token-checker/auth/middleware")),
		authenticator: authenticator,
		clientType:    clientType,
	}
}

// IsValidAccessToken checks if the access token is valid for the given user ID.
func (tc *TokenChecker) IsValidAccessToken(ctx context.Context, accessToken string, skipRedis bool) (bool, *operatorV1.UserTokenPayload) {
	resp, err := tc.authenticator.Authenticate(ctx, &operatorV1.ValidateTokenRequest{
		Token:         accessToken,
		TokenCategory: operatorV1.TokenCategory_ACCESS,
		ClientType:    tc.clientType,
		SkipRedis:     trans.Ptr(skipRedis),
	})
	if err != nil {
		return false, nil
	}

	if !resp.IsValid {
		return false, nil
	}

	return true, resp.Payload
}

// IsBlockedAccessToken checks if the access token is blocked for the given user ID.
func (tc *TokenChecker) IsBlockedAccessToken(ctx context.Context, accessToken string) bool {
	resp, err := tc.authenticator.Authenticate(ctx, &operatorV1.ValidateTokenRequest{
		Token:         accessToken,
		TokenCategory: operatorV1.TokenCategory_ACCESS,
		ClientType:    tc.clientType,
		SkipRedis:     trans.Ptr(true),
	})
	if err != nil {
		return true
	}
	return !resp.IsValid
}
