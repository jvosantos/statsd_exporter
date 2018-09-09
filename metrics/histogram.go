package metrics

type histogram struct {
	valBits []float64

	labels Labels
	name string
	description string
}

type Histogram interface {
	// Set sets the Gauge to an arbitrary value.
	Observe(float64)

	Name() string
	Value() []float64
	Description() string
	Labels() Labels
}

func NewHistogram(name, description string, labels Labels) Histogram {
	result := &histogram{name: name, description: description, labels: labels}
	return result
}

func (h *histogram) Name() string {
	return h.name
}

func (h *histogram) Value() []float64 {
	return h.valBits
}

func (h *histogram) Description() string {
	return h.description
}

func (h *histogram) Labels() Labels {
	return h.labels
}

func (h *histogram) Observe(val float64) {
	h.valBits = append(h.valBits, val)
}
