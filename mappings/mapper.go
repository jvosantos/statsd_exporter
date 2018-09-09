package mappings

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
	"github.com/jvosantos/statsd_exporter/metrics"
	"github.com/golang/glog"
)

var (
	statsdMetricRE    = `[a-zA-Z_](-?[a-zA-Z0-9_])+`
	templateReplaceRE = `(\$\{?\d+\}?)`

	metricLineRE = regexp.MustCompile(`^(\*\.|` + statsdMetricRE + `\.)+(\*|` + statsdMetricRE + `)$`)
	metricNameRE = regexp.MustCompile(`^([a-zA-Z_]|` + templateReplaceRE + `)([a-zA-Z0-9_]|` + templateReplaceRE + `)*$`)
	labelNameRE  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]+$`)
)

type mapperConfigDefaults struct {
	TimerType timerType `yaml:"timer_type"`
	MatchType matchType `yaml:"match_type"`
}

type MetricMapper struct {
	Defaults mapperConfigDefaults `yaml:"defaults"`
	Mappings []MetricMapping      `yaml:"mappings"`
	mutex    sync.Mutex
}

type MetricMapping struct {
	Match           string              `yaml:"match"`
	Name            string              `yaml:"name"`
	regex           *regexp.Regexp
	Labels          metrics.Labels 	    `yaml:"labels"`
	TimerType       timerType           `yaml:"timer_type"`
	MatchType       matchType           `yaml:"match_type"`
	HelpText        string              `yaml:"help"`
	Action          actionType          `yaml:"action"`
	MatchMetricType metrics.MetricType  `yaml:"match_metric_type"`
}

func (m *MetricMapper) InitFromYAMLString(fileContents string) error {
	var n MetricMapper

	if err := yaml.Unmarshal([]byte(fileContents), &n); err != nil {
		return err
	}

	if n.Defaults.MatchType == matchTypeDefault {
		n.Defaults.MatchType = matchTypeGlob
	}

	for i := range n.Mappings {
		glog.V(100).Infoln("parsing mapping", n.Mappings[i].Name)
		currentMapping := &n.Mappings[i]

		// check that label is correct
		for k := range currentMapping.Labels {
			if !labelNameRE.MatchString(k) {
				return fmt.Errorf("invalid label key: %s", k)
			}
		}

		if currentMapping.Name == "" {
			return fmt.Errorf("line %d: metric mapping didn't set a metric name", i)
		}

		if !metricNameRE.MatchString(currentMapping.Name) {
			return fmt.Errorf("metric name '%s' doesn't match regex '%s'", currentMapping.Name, metricNameRE)
		}

		if currentMapping.MatchType == "" {
			currentMapping.MatchType = n.Defaults.MatchType
		}

		if currentMapping.Action == "" {
			currentMapping.Action = ActionTypeMap
		}

		if currentMapping.MatchType == matchTypeGlob {
			if !metricLineRE.MatchString(currentMapping.Match) {
				return fmt.Errorf("invalid match: %s", currentMapping.Match)
			}
			// Translate the glob-style metric match line into a proper regex that we
			// can use to match metrics later on.
			metricRe := strings.Replace(currentMapping.Match, ".", "\\.", -1)
			metricRe = strings.Replace(metricRe, "*", "([^.]*)", -1)
			if regex, err := regexp.Compile("^" + metricRe + "$"); err != nil {
				return fmt.Errorf("invalid match %s. cannot compile regex in mapping: %v", currentMapping.Match, err)
			} else {
				currentMapping.regex = regex
			}
		} else {
			if regex, err := regexp.Compile(currentMapping.Match); err != nil {
				return fmt.Errorf("invalid regex %s in mapping: %v", currentMapping.Match, err)
			} else {
				currentMapping.regex = regex
			}
		}

		if currentMapping.TimerType == "" {
			currentMapping.TimerType = n.Defaults.TimerType
		}
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.Defaults = n.Defaults
	m.Mappings = n.Mappings

	//mappingsCount.Set(float64(len(n.Mappings))) // self metric

	return nil
}

func (m *MetricMapper) InitFromFile(fileName string) error {
	mappingStr, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}
	return m.InitFromYAMLString(string(mappingStr))
}

func (m *MetricMapper) GetMapping(statsdMetric string, statsdMetricType metrics.MetricType) (*MetricMapping, metrics.Labels, bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, mapping := range m.Mappings {
		matches := mapping.regex.FindStringSubmatchIndex(statsdMetric)
		if len(matches) == 0 {
			continue
		}

		mapping.Name = string(mapping.regex.ExpandString(
			[]byte{},
			mapping.Name,
			statsdMetric,
			matches,
		))

		if mt := mapping.MatchMetricType; mt != "" && mt != statsdMetricType {
			continue
		}

		labels := metrics.Labels{}
		for label, valueExpr := range mapping.Labels {
			value := mapping.regex.ExpandString([]byte{}, valueExpr, statsdMetric, matches)
			labels[label] = string(value)
		}

		return &mapping, labels, true
	}

	return nil, nil, false
}
