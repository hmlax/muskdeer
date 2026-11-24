package domain

import "errors"

const messageBelowMarketMinimum = "development event is below the venue market minimum"

// ErrBelowMarketMinimum identifies a filtered event that was never sent.
var ErrBelowMarketMinimum = errors.New(messageBelowMarketMinimum)
