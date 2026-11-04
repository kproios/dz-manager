package app

import "context"

// registerFeatures wires feature modules (RCON, MetricZ, restarts, bridge…).
func (a *App) registerFeatures(ctx context.Context) error {
	_ = ctx
	return nil
}
