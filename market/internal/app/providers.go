//go:build wireinject

package app

import (
	"github.com/google/wire"

	"github.com/hmlax/muskdeer/internal/processlog"
)

// ProviderSet defines the market graph; it is used only by Wire generation.
var ProviderSet = wire.NewSet(NewConfig, NewWorkPlan, processlog.NewLifecycle, NewSpec, New)
