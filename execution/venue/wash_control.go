package venue

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"

	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

const washControlAge = 2 * time.Second

type washControl struct {
	active   bool
	observed time.Time
}

// This control-plane loop is independent of all subscribed trade processing.
// One configuration read refreshes all current WashTrade enable/revision flags.
func (r *Instances) watchWashControls(ctx context.Context) error {
	var children []*Runtime
	for _, child := range r.ordered {
		if child.cfg.Template == domain.StrategyTemplateWashTrade {
			children = append(children, child)
		}
	}
	if len(children) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}
	failed := false
	for ctx.Err() == nil {
		op, cancel := context.WithTimeout(ctx, min(r.timeout, time.Second))
		err := refreshWashControls(op, children[0].db, children)
		cancel()
		if err != nil && !failed && ctx.Err() == nil {
			if err := children[0].logger.LogAttrs(ctx, slog.LevelWarn, native.StateRecovering, slog.String("component", "wash_control")); err != nil {
				return err
			}
		}
		failed = err != nil
		if err := native.Wait(ctx, 500*time.Millisecond); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func refreshWashControls(ctx context.Context, db *gorm.DB, children []*Runtime) error {
	ids := make([]int64, 0, len(children))
	for _, child := range children {
		ids = append(ids, child.cfg.InstanceID)
	}
	var rows []*model.StrategyInstance
	err := db.WithContext(ctx).Model(&model.StrategyInstance{}).
		Select("strategy_instances.id, strategy_instances.revision").
		Joins("JOIN strategy_templates AS template ON template.id = strategy_instances.template_id AND template.revision = strategy_instances.template_revision AND template.enabled AND template.delete_at IS NULL").
		Joins("JOIN instruments AS instrument ON instrument.id = strategy_instances.instrument_id AND instrument.delete_at IS NULL").
		Where("strategy_instances.id IN ? AND strategy_instances.desired_running AND strategy_instances.delete_at IS NULL", ids).
		Where("strategy_instances.template = ? AND template.strategy_type = strategy_instances.template AND template.implementation = strategy_instances.implementation AND instrument.status = ?", domain.StrategyTemplateWashTrade, domain.InstrumentStatusTrading).
		Find(&rows).Error
	current := make(map[int64]int64, len(rows))
	if err == nil {
		for _, row := range rows {
			current[row.Id] = row.Revision
		}
	}
	now := time.Now()
	for _, child := range children {
		child.washControl.Store(&washControl{
			active:   err == nil && current[child.cfg.InstanceID] == child.cfg.Revision,
			observed: now,
		})
	}
	return err
}
