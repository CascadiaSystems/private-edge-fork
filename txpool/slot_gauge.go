package txpool

import (
	"sync/atomic"

	"github.com/0xPolygon/polygon-edge/types"
)

// Gauge for measuring pool capacity in slots
type slotGauge struct {
	height uint64 // amount of slots currently occupying the pool
	max    uint64 // max limit
}

// read returns the current height of the gauge.
func (g *slotGauge) read() uint64 {
	return atomic.LoadUint64(&g.height)
}

// 	increase increases the height of the gauge by the specified slots amount
//	and returns the updated value.
func (g *slotGauge) increase(slots uint64) uint64 {
	return atomic.AddUint64(&g.height, slots)
}

// 	decrease decreases the height of the gauge by the specified slots amount
//	and returns the updates value
func (g *slotGauge) decrease(slots uint64) uint64 {
	return atomic.AddUint64(&g.height, ^(slots - 1))
}

func (g *slotGauge) stable(amount uint64) bool {
	return amount < uint64(0.8*float64(g.max))
}

// slotsRequired calculates the number of slots required for given transaction(s).
func slotsRequired(txs ...*types.Transaction) uint64 {
	slots := uint64(0)
	for _, tx := range txs {
		slots += (tx.Size() + txSlotSize - 1) / txSlotSize
	}

	return slots
}
