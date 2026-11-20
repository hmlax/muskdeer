package muskdeer

import (
	"strconv"
	"strings"
)

// AccountScopePrefix retains the persisted SDK account identity prefix.
const AccountScopePrefix = "himalaya/"

// TradingID preserves the full positive int64 identifier supplied as decimal text.
func TradingID(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}

// AccountScope returns the stable persisted user/fund identity.
func AccountScope(user, fund int64) string {
	return UserScopePrefix(user) + strconv.FormatInt(fund, 10)
}

// UserScopePrefix returns the persisted prefix for one user.
func UserScopePrefix(user int64) string {
	return AccountScopePrefix + strconv.FormatInt(user, 10) + "/"
}

// AccountUser parses the user from a persisted account scope.
func AccountUser(account string) (int64, bool) {
	parts := strings.Split(account, "/")
	if len(parts) != 3 || parts[0]+"/" != AccountScopePrefix {
		return 0, false
	}
	user, uok := TradingID(parts[1])
	_, fok := TradingID(parts[2])
	return user, uok && fok
}
