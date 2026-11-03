package app

import (
	"io"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/processlog"
)

// NewSpec binds execution work and logging to the sole Deer owner.
func NewSpec(cfg *Config, work *WorkPlan, logging *processlog.Lifecycle, output io.Writer) deer.ServiceSpec {
	spec := deer.ServiceSpec{
		LogOutput:           output,
		SetupProcessLogging: logging.Install,
		Resources: []deer.Initializer{
			{
				Name: "execution.lifecycle",
				Open: logging.Open,
			},
		},
	}
	if cfg.service.GetBusiness().GetEnabled() {
		spec.Workers = work.configure
		spec.RegisterGRPC = work.register
	}
	return spec
}
