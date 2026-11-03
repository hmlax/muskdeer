package business

const maxRuntimeAccounts = 16

func runtimeAccountsValid(accounts []string) bool {
	if len(accounts) == 0 || len(accounts) > maxRuntimeAccounts {
		return false
	}
	seen := make(map[string]struct{}, len(accounts))
	for _, account := range accounts {
		if !runtimeAccountID(account) {
			return false
		}
		if _, duplicate := seen[account]; duplicate {
			return false
		}
		seen[account] = struct{}{}
	}
	return true
}

func runtimeAccountID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for index := range len(value) {
		c := value[index]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':' {
			continue
		}
		return false
	}
	return true
}
