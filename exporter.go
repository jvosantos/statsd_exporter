package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/golang/glog"
	"github.com/jvosantos/statsd_exporter/metrics"
	"hash/fnv"
	"sort"
	"github.com/olivere/elastic"
	"github.com/jvosantos/statsd_exporter/mappings"
	"time"
)

const (
	defaultHelp = "Metric autogenerated by statsd_exporter."
	regErrF     = "A change of configuration created inconsistent metrics for " +
		"%q. You have to restart the statsd_exporter, and you should " +
		"consider the effects on your monitoring setup. Error: %s"
)

var (
	hash   = fnv.New64a()
	strBuf bytes.Buffer // Used for hashing.
	intBuf = make([]byte, 8)
)

func labelsToSignature(labels map[string]string) uint64 {
	if len(labels) == 0 {
		return emptyLabelSignature
	}

	labelNames := make([]string, 0, len(labels))
	for labelName := range labels {
		labelNames = append(labelNames, labelName)
	}
	sort.Strings(labelNames)

	sum := hashNew()
	for _, labelName := range labelNames {
		sum = hashAdd(sum, labelName)
		sum = hashAddByte(sum, separatorByte)
		sum = hashAdd(sum, labels[labelName])
		sum = hashAddByte(sum, separatorByte)
	}
	return sum
}

func hashNameAndLabels(name string, labels metrics.Labels) uint64 {
	hash.Reset()
	strBuf.Reset()
	strBuf.WriteString(name)
	hash.Write(strBuf.Bytes())
	binary.BigEndian.PutUint64(intBuf, labelsToSignature(labels))
	hash.Write(intBuf)
	return hash.Sum64()
}

type MetricDocument struct {
	Timestamp	time.Time		`json:"@timestamp"`
	Name        string			`json:"name"`
	Description string			`json:"description"`
	Value       float64			`json:"value"`
	Labels      metrics.Labels	`json:"labels"`
	MetricType  string			`json:"metricType"`
}

type CounterContainer struct {
	Elements map[uint64]metrics.Counter
}

func NewCounterContainer() *CounterContainer {
	return &CounterContainer{
		Elements: make(map[uint64]metrics.Counter),
	}
}

func (c *CounterContainer) Get(metricName string, labels metrics.Labels, help string) (metrics.Counter, error) {
	hash := hashNameAndLabels(metricName, labels)
	counter, ok := c.Elements[hash]
	if !ok {
		counter = metrics.NewCounter(metricName, help, labels)

		c.Elements[hash] = counter
	}
	return counter, nil
}

type GaugeContainer struct {
	Elements map[uint64]metrics.Gauge
}

func NewGaugeContainer() *GaugeContainer {
	return &GaugeContainer{
		Elements: make(map[uint64]metrics.Gauge),
	}
}

func (c *GaugeContainer) Get(metricName string, labels metrics.Labels, help string) (metrics.Gauge, error) {
	hash := hashNameAndLabels(metricName, labels)
	gauge, ok := c.Elements[hash]
	if !ok {
		gauge = metrics.NewGauge(metricName, help, labels)

		c.Elements[hash] = gauge
	}
	return gauge, nil
}

type HistogramContainer struct {
	Elements map[uint64]metrics.Histogram
}

func NewHistogramContainer() *HistogramContainer {
	return &HistogramContainer{
		Elements: make(map[uint64]metrics.Histogram),
	}
}

func (c *HistogramContainer) Get(metricName string, labels metrics.Labels, help string) (metrics.Histogram, error) {
	hash := hashNameAndLabels(metricName, labels)
	histogram, ok := c.Elements[hash]
	if !ok {
		histogram = metrics.NewHistogram(metricName, help, labels)

		c.Elements[hash] = histogram
	}
	return histogram, nil
}

type Exporter struct {
	Counters      *CounterContainer
	Gauges        *GaugeContainer
	Histograms    *HistogramContainer
	mapper        *mappings.MetricMapper
	elasticBulkProcessor *elastic.BulkProcessor
	elasticIndex  string
}

func NewExporter(mapper *mappings.MetricMapper, processor *elastic.BulkProcessor, index string) *Exporter {
	return &Exporter{
		Counters:      NewCounterContainer(),
		Gauges:        NewGaugeContainer(),
		Histograms:    NewHistogramContainer(),
		mapper:        mapper,
		elasticBulkProcessor: processor,
		elasticIndex:  index,
	}
}

func (b *Exporter) Listen(hierarchicalEventsChannel <-chan metrics.Events) {
	for {
		select {
		case hierarchicalEvents, ok := <-hierarchicalEventsChannel:
			if !ok {
				glog.V(10).Info("Channel is closed. Break out of Exporter.Listener.")
				return
			}
			b.processHierarchicalEvents(hierarchicalEvents)
		}
	}
}

func (b *Exporter) flush() {
	glog.V(10).Info("Flushing metrics")

	time.Now()
	time.Now().Format("2006.01.02")

	for hash, counter := range b.Counters.Elements {
		glog.V(100).Info(counter.Name(), counter.Value(), counter.Labels())
		b.elasticBulkProcessor.Add(elastic.NewBulkIndexRequest().Index(b.elasticIndex).Type("metric").Doc(MetricDocument{
			Timestamp:	 time.Now(),
			Name:        counter.Name(),
			Description: counter.Description(),
			MetricType:  "counter",
			Value:       counter.Value(),
			Labels:      counter.Labels(),
		}))
		delete(b.Counters.Elements, hash)
	}

	for hash, gauge := range b.Gauges.Elements {
		glog.V(100).Info(gauge.Name(), gauge.Value(), gauge.Labels())
		delete(b.Gauges.Elements, hash)
	}

	for hash, timer := range b.Histograms.Elements {
		glog.V(100).Info(timer.Name(), timer.Value(), timer.Labels())
		delete(b.Histograms.Elements, hash)
	}
}

func (b *Exporter) processHierarchicalEvents(hierarchicalEvents metrics.Events) {
	for _, hierarchicalEvent := range hierarchicalEvents {
		var help string
		metricName := ""
		eventLabels := hierarchicalEvent.Labels()

		// Retrieve mapping of current hierarchical event being processed and extract Labels
		mapping, labels, present := b.mapper.GetMapping(hierarchicalEvent.MetricName(), hierarchicalEvent.MetricType())
		if mapping == nil {
			mapping = &mappings.MetricMapping{}
		}

		if mapping.Action == mappings.ActionTypeDrop {
			continue
		}

		if mapping.HelpText == "" {
			help = defaultHelp
		} else {
			help = mapping.HelpText
		}
		if present {
			metricName = metrics.EscapeMetricName(mapping.Name)
			for label, value := range labels {
				eventLabels[label] = value
			}
		} else {
			//eventsUnmapped.Inc() // self metric
			metricName = metrics.EscapeMetricName(hierarchicalEvent.MetricName())
		}

		index := b.elasticIndex + time.Now().Format("-2006.01.02")
		glog.Infoln("index:", index)

		switch ev := hierarchicalEvent.(type) {
		case *metrics.CounterEvent:
			// We don't accept negative values for counters. Incrementing the counter with a negative number
			// will cause the exporter to panic. Instead we will warn and continue to the next hierarchicalEvent.
			if hierarchicalEvent.Value() < 0.0 {
				glog.V(10).Infof("Counter %q is: '%f' (counter must be non-negative Value)", metricName, hierarchicalEvent.Value())
				//eventStats.WithLabelValues("illegal_negative_counter").Inc() // self metric
				continue
			}

			b.elasticBulkProcessor.Add(
				elastic.NewBulkIndexRequest().
					Index(index).
					Type("doc").
					Doc(MetricDocument{
						Timestamp:	 time.Now(),
						Name:        metricName,
						Description: help,
						MetricType:  "counter",
						Value:       hierarchicalEvent.Value(),
						Labels:      eventLabels,
				}))

			//counter, err := b.Counters.Get(
			//	metricName,
			//	eventLabels,
			//	help,
			//)
			//if err == nil {
			//	counter.Add(hierarchicalEvent.Value())
			//
			//	//eventStats.WithLabelValues("counter").Inc() // self metric
			//} else {
			//	glog.V(10).Infof(regErrF, metricName, err)
			//	//conflictingEventStats.WithLabelValues("counter").Inc() // self metric
			//}

		case *metrics.GaugeEvent:
			gauge, err := b.Gauges.Get(
				metricName,
				labels,
				help,
			)

			if err == nil {
				if ev.Relative() {
					gauge.Add(hierarchicalEvent.Value())
				} else {
					gauge.Set(hierarchicalEvent.Value())
				}

				b.elasticBulkProcessor.Add(
					elastic.NewBulkIndexRequest().
						Index(index).
						Type("doc").
						Doc(MetricDocument{
							Timestamp:	 time.Now(),
							Name:        metricName,
							Description: help,
							MetricType:  "gauge",
							Value:       gauge.Value(),
							Labels:      eventLabels,
					}))

				//eventStats.WithLabelValues("gauge").Inc()  // self metric
			} else {
				glog.V(10).Infof(regErrF, metricName, err)
				//conflictingEventStats.WithLabelValues("gauge").Inc() // self metric
			}

		case *metrics.TimerEvent:
			t := mappings.TimerTypeDefault
			if mapping != nil {
				t = mapping.TimerType
			}
			if t == mappings.TimerTypeDefault {
				t = b.mapper.Defaults.TimerType
			}

			switch t {
			case mappings.TimerTypeDefault, mappings.TimerTypeRaw:
				b.elasticBulkProcessor.Add(
					elastic.NewBulkIndexRequest().
						Index(index).
						Type("doc").
						Doc(MetricDocument{
							Timestamp:	 time.Now(),
							Name:        metricName,
							Description: help,
							MetricType:  "raw_timer",
							Value:       hierarchicalEvent.Value(),
							Labels:      eventLabels,
					}))

				//histogram, err := b.Histograms.Get(
				//	metricName,
				//	labels,
				//	help,
				//)

				//if err == nil {
				//	histogram.Observe(hierarchicalEvent.Value())
					//eventStats.WithLabelValues("timer").Inc() // self metric
				//} else {
				//	glog.V(10).Infof(regErrF, metricName, err)
					//conflictingEventStats.WithLabelValues("timer").Inc() // self metric
				//}
			default:
				panic(fmt.Sprintf("unknown timer type '%s'", t))
			}

		default:
			glog.V(10).Infoln("Unsupported hierarchicalEvent type")
			//eventStats.WithLabelValues("illegal").Inc() // self metric
		}
	}
}
