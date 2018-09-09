package metrics

import (
	"fmt"
	"time"
)

type Events []Event
type Labels map[string]string

type Event interface {
	Timestamp() time.Time
	MetricName() string
	Value() float64
	Labels() Labels
	MetricType() MetricType
}

func NewEvent(statType, metric string, value float64, relative bool, labels Labels) (Event, error) {
	switch statType {
	case "c":
		return &CounterEvent{
			timestamp:  time.Now(),
			metricName: metric,
			value:      float64(value),
			labels:     labels,
		}, nil
	case "g":
		return &GaugeEvent{
			timestamp:  time.Now(),
			metricName: metric,
			value:      float64(value),
			relative:   relative,
			labels:     labels,
		}, nil
	case "ms", "h":
		return &TimerEvent{
			timestamp:  time.Now(),
			metricName: metric,
			value:      float64(value),
			labels:     labels,
		}, nil
	case "s":
		return nil, fmt.Errorf("no support for StatsD sets")
	default:
		return nil, fmt.Errorf("bad stat type %s", statType)
	}
}

type CounterEvent struct {
	timestamp  time.Time
	metricName string
	value      float64
	labels     Labels
}

func NewCounterEvent(metricName string, value float64, labels Labels) CounterEvent {
	return CounterEvent{metricName:metricName, value:value, labels:labels}
}
func (c *CounterEvent) MetricName() string        { return c.metricName }
func (c *CounterEvent) Value() float64            { return c.value }
func (c *CounterEvent) Labels() Labels 			  { return c.labels }
func (c *CounterEvent) MetricType() MetricType    { return metricTypeCounter }
func (c *CounterEvent) Timestamp() time.Time	  { return c.timestamp }

type GaugeEvent struct {
	timestamp  time.Time
	metricName string
	value      float64
	relative   bool
	labels     Labels
}

func NewGaugeEvent(metricName string, value float64, relative bool, labels Labels) GaugeEvent {
	return GaugeEvent{metricName:metricName, value:value, relative:relative, labels:labels}
}
func (g *GaugeEvent) MetricName() string        { return g.metricName }
func (g *GaugeEvent) Value() float64            { return g.value }
func (g *GaugeEvent) Labels() Labels			{ return g.labels }
func (g *GaugeEvent) MetricType() MetricType    { return metricTypeGauge }
func (g *GaugeEvent) Relative() bool 			{ return g.relative }
func (g *GaugeEvent) Timestamp() time.Time	    { return g.timestamp }

type TimerEvent struct {
	timestamp  time.Time
	metricName string
	value      float64
	labels     Labels
}

func NewTimerEvent(metricName string, value float64, labels Labels) TimerEvent {
	return TimerEvent{metricName:metricName, value:value, labels:labels}
}
func (t *TimerEvent) MetricName() string        { return t.metricName }
func (t *TimerEvent) Value() float64            { return t.value }
func (t *TimerEvent) Labels() Labels 			{ return t.labels }
func (t *TimerEvent) MetricType() MetricType    { return metricTypeTimer }
func (t *TimerEvent) Timestamp() time.Time	    { return t.timestamp }
