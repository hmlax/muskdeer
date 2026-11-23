package domain

// Account activation failures have a stable public identity across transports.
const (
	AccountDisabledReason  = "TRADING_ACCOUNT_DISABLED"
	AccountDisabledMessage = "enable the trading account before starting a strategy"
)
