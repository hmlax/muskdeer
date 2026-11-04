package app

import (
	"io"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/processlog"
)

// NewSpec binds cron work and logging to the sole Deer owner.
func NewSpec(logging *processlog.Lifecycle, output io.Writer) deer.ServiceSpec {
	spec := deer.ServiceSpec{
		LogOutput:           output,
		SetupProcessLogging: logging.Install,
		Resources: []deer.Initializer{
			{
				Name: "cron.lifecycle",
				Open: logging.Open,
			},
		},
	}
	return spec
}
