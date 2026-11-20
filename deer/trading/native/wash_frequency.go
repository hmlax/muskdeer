package native

import (
	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
)

// WashSourceQueueCapacity absorbs short source bursts without an unbounded
// backlog. It bounds pending observations, never instance count or send rate.
const WashSourceQueueCapacity = 64

// WashFollowsSource selects the explicit zero-interval development trade mode.
func WashFollowsSource(policy *common.VenuePolicy) bool {
	tape := policy.GetSyntheticTape()
	return tape != nil && tape.DevelopmentOnly && tape.Mode == config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE &&
		tape.Interval != nil && tape.Interval.AsDuration() == 0
}
