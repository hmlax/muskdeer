package testentry

import "context"

// IsCancellation reports cancellation alone, preserving any concurrent cleanup
// or business failure in a joined error.
func IsCancellation(err error) bool {
	if err == context.Canceled {
		return true
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		children := group.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !IsCancellation(child) {
				return false
			}
		}
		return true
	}
	if wrapper, ok := err.(interface{ Unwrap() error }); ok {
		return IsCancellation(wrapper.Unwrap())
	}
	return false
}
