package metrics

import (
	"sync/atomic"
	"math"
)

type gauge struct {
	valBits uint64

	labels Labels
	name string
	description string

}

type Gauge interface {
	// Set sets the Gauge to an arbitrary value.
	Set(float64)
	// Add adds the given value to the Gauge. (The value can be negative,
	// resulting in a decrease of the Gauge.)
	Add(float64)

	Name() string
	Value() float64
	Description() string
	Labels() Labels
}

func NewGauge(name, description string, labels Labels) Gauge {
	result := &gauge{name: name, description: description, labels: labels}
	return result
}

func (g *gauge) Name() string {
	return g.name
}

func (g *gauge) Value() float64 {
	return math.Float64frombits(atomic.LoadUint64(&g.valBits))
}

func (g *gauge) Description() string {
	return g.description
}

func (g *gauge) Labels() Labels {
	return g.labels
}

func (g *gauge) Set(val float64) {
	atomic.StoreUint64(&g.valBits, math.Float64bits(val))
}

func (g *gauge) Add(val float64){
	for {
		oldBits := atomic.LoadUint64(&g.valBits)
		newBits := math.Float64bits(math.Float64frombits(oldBits) + val)
		if atomic.CompareAndSwapUint64(&g.valBits, oldBits, newBits) {
			return
		}
	}
}
