package metrics

import (
	"fmt"
	"regexp"
)

type MetricType string

const (
	metricTypeCounter MetricType = "counter"
	metricTypeGauge   MetricType = "gauge"
	metricTypeTimer   MetricType = "timer"
)

var (
	illegalCharsRE = regexp.MustCompile(`[^a-zA-Z0-9_]`)
)

func EscapeMetricName(metricName string) string {
	// If a metric starts with a digit, prepend an underscore.
	if metricName[0] >= '0' && metricName[0] <= '9' {
		metricName = "_" + metricName
	}

	// Replace all illegal metric chars with underscores.
	metricName = illegalCharsRE.ReplaceAllString(metricName, "_")
	return metricName
}

func (m *MetricType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var v string
	if err := unmarshal(&v); err != nil {
		return err
	}

	switch MetricType(v) {
	case metricTypeCounter:
		*m = metricTypeCounter
	case metricTypeGauge:
		*m = metricTypeGauge
	case metricTypeTimer:
		*m = metricTypeTimer
	default:
		return fmt.Errorf("invalid metric type '%s'", v)
	}
	return nil
}
