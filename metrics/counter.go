package metrics

import (
	"errors"
	"sync/atomic"
	"math"
)

type counter struct {
	valBits uint64
	valInt uint64

	labels Labels
	name string
	description string
}

type Counter interface {
	Inc()
	Add(float64)

	Name() string
	Value() float64
	Description() string
	Labels() Labels
}

func NewCounter(name, description string, labels Labels) Counter {
	result := &counter{name: name, description: description, labels: labels}
	return result
}

func (c *counter) Name() string {
	return c.name
}

func (c *counter) Value() float64 {
	fval := math.Float64frombits(atomic.LoadUint64(&c.valBits))
	ival := atomic.LoadUint64(&c.valInt)
	val := fval + float64(ival)

	return val
}

func (c *counter) Description() string {
	return c.description
}

func (c *counter) Labels() Labels {
	return c.labels
}

func (c *counter) Add(v float64) {
	if v < 0 {
		panic(errors.New("counter cannot decresase in value"))
	}
	ival := uint64(v)
	if float64(ival) == v {
		atomic.AddUint64(&c.valInt, ival)
		return
	}

	for {
		oldBits := atomic.LoadUint64(&c.valBits)
		newBits := math.Float64bits(math.Float64frombits(oldBits) + v)
		if atomic.CompareAndSwapUint64(&c.valBits, oldBits, newBits) {
			return
		}
	}
}

func (c *counter) Inc() {
	atomic.AddUint64(&c.valInt, 1)
}
