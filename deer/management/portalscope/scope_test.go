package portalscope

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMerchantHeaderRejectsAmbiguousValues(t *testing.T) {
	for _, values := range [][]string{{"one", "two"}, {"one,two"}, {"a\r\nX-Other: yes"}, {"a/b"}} {
		_, err := ParseCode(values)
		require.Error(t, err)
	}
	code, err := ParseCode([]string{" AbC123_- "})
	require.NoError(t, err)
	require.Equal(t, "AbC123_-", code)
}

func TestPortalIdentityAndCookiesRemainSeparate(t *testing.T) {
	base := context.Background()
	platform := NewContext(base, Scope{})
	merchant := NewContext(base, Scope{
		TenantID: 7,
		Code:     "MerchantA",
	})
	other := NewContext(base, Scope{
		TenantID: 8,
		Code:     "MerchantB",
	})
	require.NoError(t, CheckTenant(platform, 0))
	require.NoError(t, CheckTenant(merchant, 7))
	require.Error(t, CheckTenant(platform, 7))
	require.Error(t, CheckTenant(merchant, 0))
	require.Error(t, CheckTenant(other, 7))
	require.NoError(t, CheckTenant(base, 7), "non-HTTP internal calls retain their existing contract")
	require.Equal(t, "refresh_token", CookieName(platform, "refresh_token"))
	require.Equal(t, "refresh_token_MerchantA", CookieName(merchant, "refresh_token"))
	require.NotEqual(t, CookieName(merchant, "refresh_token"), CookieName(other, "refresh_token"))
}
