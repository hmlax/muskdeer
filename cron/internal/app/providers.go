//go:build wireinject

package app

import (
	"github.com/google/wire"

	"github.com/hmlax/muskdeer/deer/processlog"
)

// ProviderSet defines the cron graph; it is used only by Wire generation.
var ProviderSet = wire.NewSet(NewConfig, processlog.NewLifecycle, NewSpec, New)
