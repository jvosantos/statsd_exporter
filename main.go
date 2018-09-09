package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/golang/glog"
	"github.com/howeyc/fsnotify"
	"github.com/jvosantos/statsd_exporter/mappings"
	"github.com/jvosantos/statsd_exporter/metrics"
	"github.com/jvosantos/statsd_exporter/statsd"
	"github.com/olivere/elastic"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"
)

var (
	statsdListenUDP     	 	= flag.String("statsd.listen-udp", "", "The UDP address on which to receive statsd metric lines. \"\" disables it.")
	readBuffer          	 	= flag.Int("statsd.read-buffer", 0, "Size (in bytes) of the operating system's transmit read buffer associated with the UDP connection. Please make sure the kernel parameters net.core.rmem_max is set to a Value greater than the Value specified.")
	statsdListenTCP     	 	= flag.String("statsd.listen-tcp", "", "The TCP address on which to receive statsd metric lines. \"\" disables it.")

	mappingConfig       	 	= flag.String("mapping-config", "mappings.yaml", "Metric mapping configuration file name.")

	elasticHost				 	= flag.String("elasticsearch.url", "localhost:9200", "The URL endpoints of the Elasticsearch nodes. Multiple urls can be added separated by a comma. Notice that when sniffing is enabled, these URLs are used to initially sniff the cluster on startup.")
	elasticUsername			 	= flag.String("elasticsearch.username", "", "The username to be used as basic authentication on Elasticsearch requests.")
	elasticPassword			 	= flag.String("elasticsearch.password", "", "The password to be used as basic authentication on Elasticsearch requests.")
	elasticIndex			 	= flag.String("elasticsearch.index", "statsdexporter", "The Name of the index to push metrics to. Defaults to \"statsdexporter\".")
	elasticIndexTemplate		= flag.String("elasticsearch.template", "", "Elastic Search Index template file name. Should be in json format.")
	elasticIndexTemplateName 	= flag.String("elasticsearch.template-name", "statsdexporter", "Index template name, defaults to index name.")
	elasticWorkers			 	= flag.Int("elasticsearch.workers", 1, "Workers is the number of concurrent workers allowed to be executed. Defaults to 1 and must be greater or equal to 1.")
	elasticActionsThreshold	 	= flag.Int("elasticsearch.actions-threshold", 1000, "BulkActions specifies when to flush based on the number of actions currently added. Defaults to 1000 and can be set to -1 to be disabled.")
	elasticSize				 	= flag.Int("elasticsearch.size-threshold", 5000, "Bulk Size specifies when to flush based on the size (in bytes) of the actions currently added. Defaults to 5 MB and can be set to -1 to be disabled.")
	elasticFlushInterval	 	= flag.Duration("elasticsearch.flush-interval", 30 * time.Second, "Flush Interval specifies when to flush at the end of the given interval. Defaults to 30s and can be set to 0s to be disabled.")
)

func watchMappingConfig(fileName string, mapper *mappings.MetricMapper) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		glog.Fatal(err)
	}

	err = watcher.WatchFlags(fileName, fsnotify.FSN_MODIFY)
	if err != nil {
		glog.Fatal(err)
	}

	for {
		select {
		case ev := <-watcher.Event:
			glog.Infof("Config file changed (%s), attempting reload", ev)
			err = mapper.InitFromFile(fileName)
			if err != nil {
				glog.Errorln("Error reloading config:", err)
				//configLoads.WithLabelValues("failure").Inc() // self metric
			} else {
				glog.Infoln("Config reloaded successfully")
				//configLoads.WithLabelValues("success").Inc() // self metric
			}
			// Re-add the file watcher since it can get lost on some changes. E.g.
			// saving a file with vim results in a RENAME-MODIFY-DELETE event
			// sequence, after which the newly written file is no longer watched.
			_ = watcher.WatchFlags(fileName, fsnotify.FSN_MODIFY)
		case err := <-watcher.Error:
			glog.Errorln("Error watching config:", err)
		}
	}
}

func watchElasticTemplateConfig(filename string, client *elastic.Client) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		glog.Fatal(err)
	}

	err = watcher.WatchFlags(filename, fsnotify.FSN_MODIFY)
	if err != nil {
		glog.Fatal(err)
	}

	for {
		select {
		case ev := <-watcher.Event:
			glog.Infof("Template file changed (%s), attempting to update template", ev)
			err = putIndexTemplate(filename, client)
			if err != nil {
				glog.Errorln("Error reloading config:", err)
				//configLoads.WithLabelValues("failure").Inc() // self metric
			} else {
				glog.Infoln("Config reloaded successfully")
				//configLoads.WithLabelValues("success").Inc() // self metric
			}
			// Re-add the file watcher since it can get lost on some changes. E.g.
			// saving a file with vim results in a RENAME-MODIFY-DELETE event
			// sequence, after which the newly written file is no longer watched.
			_ = watcher.WatchFlags(filename, fsnotify.FSN_MODIFY)
		case err := <-watcher.Error:
			glog.Errorln("Error watching template file:", err)
		}
	}
}

func initElasticSearchClient() (*elastic.Client, *elastic.BulkProcessor) {
	var  elasticClientOptions []elastic.ClientOptionFunc

	if *elasticUsername != "" {
		elasticClientOptions = append(elasticClientOptions, elastic.SetBasicAuth(*elasticUsername, *elasticPassword))
	}

	if *elasticHost != "" {
		elasticClientOptions = append(elasticClientOptions, elastic.SetURL(strings.Split(*elasticHost, ",")...))
	}

	elasticClientOptions = append(elasticClientOptions, elastic.SetErrorLog(log.New(os.Stderr, "elastic", log.LstdFlags)))
	elasticClientOptions = append(elasticClientOptions, elastic.SetInfoLog(log.New(os.Stdout, "elastic", log.LstdFlags)))
	elasticClientOptions = append(elasticClientOptions, elastic.SetSniff(false))

	elasticClient, err := elastic.NewClient(elasticClientOptions...)
	if err != nil {
		glog.Fatal("Error creating elastic client:", err)
	}

	glog.V(100).Infoln("Created elastic client")

	elasticClient.BulkProcessor()

	elasticBulkProcessor, err := elasticClient.BulkProcessor().
		Workers(*elasticWorkers).
		BulkActions(*elasticActionsThreshold).
		BulkSize(*elasticSize).
		FlushInterval(*elasticFlushInterval).
		Do(context.Background())

	if err != nil {
		glog.Fatal("Error creating elastic bulk processor:", err)
	}

	return elasticClient, elasticBulkProcessor
}

func putIndexTemplate(filename string, client *elastic.Client) error {
	if filename == "" {
		glog.V(100).Info("Skipping creation of index template.")
		return nil
	}

	glog.Infof("Creating template %s from file %s", *elasticIndexTemplateName, *elasticIndexTemplate)
	templateStr, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	indicesPutTemplateResponse, err := client.IndexPutTemplate(*elasticIndexTemplateName).BodyString(string(templateStr)).Do(context.Background())

	if err != nil {
		return err
	}

	if indicesPutTemplateResponse.Acknowledged == false {
		return fmt.Errorf("template request for index \"%s\" not acknowledged", indicesPutTemplateResponse.Index)
	}

	return nil
}

func main() {
	flag.Parse()

	if *statsdListenUDP == "" && *statsdListenTCP == "" {
		glog.Fatalln("At least one of UDP/TCP listeners must be specified.")
	}

	glog.Infoln("Starting StatsD -> ElasticSearch Exporter")
	glog.Infof("Accepting StatsD Traffic: UDP %v, TCP %v", *statsdListenUDP, *statsdListenTCP)

	events := make(chan metrics.Events, 1024)
	defer close(events)

	if *statsdListenUDP != "" {
		sul := statsd.NewStatsDUDPListener(*statsdListenUDP, *readBuffer)

		defer sul.Close()

		go sul.Listen(events)
		glog.V(10).Infoln("Started statsd udp")
	}

	if *statsdListenTCP != "" {
		stl := statsd.NewStatsDTCPListener(*statsdListenTCP)

		defer stl.Close()

		go stl.Listen(events)
		glog.V(10).Infoln("Started statsd tcp")
	}

	mapper := &mappings.MetricMapper{}
	if *mappingConfig != "" {
		err := mapper.InitFromFile(*mappingConfig)
		if err != nil {
			glog.Fatal("Error loading config:", err)
		}
		go watchMappingConfig(*mappingConfig, mapper)
	}

	glog.V(100).Infoln("Creating elastic client")

	elasticClient, elasticBulkProcessor := initElasticSearchClient()

	defer elasticClient.Stop()
	defer elasticBulkProcessor.Flush()

	if *elasticIndexTemplate != "" {
		putIndexTemplate(*elasticIndexTemplate, elasticClient)

		go watchElasticTemplateConfig(*elasticIndexTemplate, elasticClient)
	}

	exporter := NewExporter(mapper, elasticBulkProcessor, *elasticIndex)
	exporter.Listen(events)
}
