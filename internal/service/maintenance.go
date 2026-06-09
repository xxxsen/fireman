package service

import "sync/atomic"

// MaintenanceGate blocks mutating API traffic and job claiming during backup restore.
type MaintenanceGate struct {
	active atomic.Bool
}

func (g *MaintenanceGate) Enter() { g.active.Store(true) }

func (g *MaintenanceGate) Leave() { g.active.Store(false) }

func (g *MaintenanceGate) Active() bool { return g.active.Load() }
