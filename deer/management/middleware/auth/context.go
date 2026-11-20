package auth

import (
	"context"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type ctxKey string

var (
	authClaimsContextKey = ctxKey("authn-claims")
)

func NewContext(parent context.Context, claims *operatorV1.UserTokenPayload) context.Context {
	return context.WithValue(parent, authClaimsContextKey, claims)
}

func FromContext(ctx context.Context) (*operatorV1.UserTokenPayload, error) {
	claims, ok := ctx.Value(authClaimsContextKey).(*operatorV1.UserTokenPayload)
	if !ok {
		return nil, ErrMissingJwtToken
	}
	return claims, nil
}
