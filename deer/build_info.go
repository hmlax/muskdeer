package deer

import (
	"context"
	"log/slog"
)

// BuildInfo describes the executable independently of its runtime configuration.
type BuildInfo struct {
	Name    string
	Version string
	Build   string
	ID      string
}

type buildInfoOption struct{ info BuildInfo }

// WithBuildInfo copies executable metadata for the owned startup logger. It does
// not replace Nacos configuration versions or configured service identities.
func WithBuildInfo(info BuildInfo) Option { return buildInfoOption{info: info} }

func (o buildInfoOption) apply(settings *options) error {
	info := o.info
	settings.buildInfo = &info
	return nil
}

func (d *Deer) logBuildInfo(ctx context.Context, info *BuildInfo) error {
	if info == nil {
		return nil
	}
	return d.logger.LogAttrs(ctx, slog.LevelInfo, "build.info",
		slog.String("software_name", info.Name),
		slog.String("software_version", info.Version),
		slog.String("software_build", info.Build),
		slog.String("node_id", info.ID))
}
