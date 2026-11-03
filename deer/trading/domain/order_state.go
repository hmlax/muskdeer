package domain

// Terminal reports whether the order has a confirmed final state.
func (s OrderState) Terminal() bool {
	return s == OrderStateFilled || s == OrderStateCanceled || s == OrderStateRejected
}
