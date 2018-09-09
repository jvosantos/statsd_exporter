package statsd

import (
	"net"
	"github.com/golang/glog"
	"bufio"
	"io"
	"strings"
	"strconv"
	"unicode/utf8"
	"github.com/jvosantos/statsd_exporter/metrics"
)

type Listener interface {
	Listen(chan<- metrics.Events)
	Close()
}

type TCPListener struct {
	conn *net.TCPListener
}

type UDPListener struct {
	conn *net.UDPConn
}

func NewStatsDTCPListener(address string) *TCPListener {
	tcpListenAddr := tcpAddrFromString(address)
	tcpConn, err := net.ListenTCP("tcp", tcpListenAddr)
	if err != nil {
		glog.Fatal(err)
	}

	return &TCPListener{conn: tcpConn}
}

func (l *TCPListener) Listen(e chan<- metrics.Events) {
	for {
		c, err := l.conn.AcceptTCP()
		if err != nil {
			glog.Fatalf("AcceptTCP failed: %v", err)
		}
		go l.handleConn(c, e)
	}
}

func (l *TCPListener) Close() {
	l.conn.Close()
}

func NewStatsDUDPListener(address string, readBuffer int) *UDPListener {
	udpListenAddr := udpAddrFromString(address)
	udpConn, err := net.ListenUDP("udp", udpListenAddr)
	if err != nil {
		glog.Fatal(err)
	}

	if readBuffer != 0 {
		err = udpConn.SetReadBuffer(readBuffer)
		if err != nil {
			glog.Fatal("Error setting UDP read buffer:", err)
		}
	}

	return &UDPListener{conn: udpConn}
}

func (l *UDPListener) Listen(e chan<- metrics.Events) {
	buf := make([]byte, 65535)
	for {
		n, _, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			glog.Fatal(err)
		}
		l.handlePacket(buf[0:n], e)
	}
}

func (l *UDPListener) Close() {
	// do nothing as there is nothing to close
}

func (l *TCPListener) handleConn(c *net.TCPConn, e chan<- metrics.Events) {
	defer c.Close()

	//tcpConnections.Inc() // self metric

	r := bufio.NewReader(c)
	for {
		line, isPrefix, err := r.ReadLine()
		if err != nil {
			if err != io.EOF {
				//tcpErrors.Inc() // self metric
				glog.V(10).Infof("Read %s failed: %v", c.RemoteAddr(), err)
			}
			break
		}
		if isPrefix {
			//tcpLineTooLong.Inc() // self metric
			glog.V(10).Infof("Read %s failed: line too long", c.RemoteAddr())
			break
		}
		//linesReceived.Inc() // self metric
		e <- lineToEvents(string(line))
	}
}

func (l *UDPListener) handlePacket(packet []byte, e chan<- metrics.Events) {
	//udpPackets.Inc() // self metric
	lines := strings.Split(string(packet), "\n")
	events := metrics.Events{}
	for _, line := range lines {
		//linesReceived.Inc() // self metric
		events = append(events, lineToEvents(line)...)
	}
	e <- events
}

func lineToEvents(line string) metrics.Events {
	glog.V(100).Infoln(line)

	events := metrics.Events{}
	if line == "" {
		return events
	}

	elements := strings.SplitN(line, ":", 2)
	if len(elements) < 2 || len(elements[0]) == 0 || !utf8.ValidString(line) {
		//sampleErrors.WithLabelValues("malformed_line").Inc() // self metric
		glog.V(10).Infoln("Bad line from StatsD:", line)
		return events
	}
	metric := elements[0]
	var samples []string
	if strings.Contains(elements[1], "|#") {
		// using datadog extensions, disable multi-metrics
		samples = elements[1:]
	} else {
		samples = strings.Split(elements[1], ":")
	}
samples:
	for _, sample := range samples {
		//samplesReceived.Inc() // self metric
		components := strings.Split(sample, "|")
		samplingFactor := 1.0
		if len(components) < 2 || len(components) > 4 {
			//sampleErrors.WithLabelValues("malformed_component").Inc()
			glog.V(10).Infoln("Bad component on line:", line)
			continue
		}
		valueStr, statType := components[0], components[1]

		var relative = false
		if strings.Index(valueStr, "+") == 0 || strings.Index(valueStr, "-") == 0 {
			relative = true
		}

		value, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			glog.V(10).Infof("Bad value %s on line: %s", valueStr, line)
			//sampleErrors.WithLabelValues("malformed_value").Inc()  // self metric
			continue
		}

		multiplyEvents := 1
		labels := map[string]string{}
		if len(components) >= 3 {
			for _, component := range components[2:] {
				if len(component) == 0 {
					glog.V(10).Infoln("Empty component on line: ", line)
					//sampleErrors.WithLabelValues("malformed_component").Inc() // self metric
					continue samples
				}
			}

			for _, component := range components[2:] {
				switch component[0] {
				case '@':
					if statType != "c" && statType != "ms" {
						glog.V(10).Infoln("Illegal sampling factor for non-counter metric on line", line)
						//sampleErrors.WithLabelValues("illegal_sample_factor").Inc() // self metric
						continue
					}
					samplingFactor, err = strconv.ParseFloat(component[1:], 64)
					if err != nil {
						glog.V(10).Infof("Invalid sampling factor %s on line %s", component[1:], line)
						//sampleErrors.WithLabelValues("invalid_sample_factor").Inc() // self metric
					}
					if samplingFactor == 0 {
						samplingFactor = 1
					}

					if statType == "c" {
						value /= samplingFactor
					} else if statType == "ms" {
						multiplyEvents = int(1 / samplingFactor)
					}
				case '#':
					labels = parseDogStatsDTagsToLabels(component)
				default:
					glog.V(10).Infof("Invalid sampling factor or tag section %s on line %s", components[2], line)
					//sampleErrors.WithLabelValues("invalid_sample_factor").Inc() // self metric
					continue
				}
			}
		}

		for i := 0; i < multiplyEvents; i++ {
			event, err := metrics.NewEvent(statType, metric, value, relative, labels)
			if err != nil {
				glog.V(10).Infof("Error building event on line %s: %s", line, err)
				//sampleErrors.WithLabelValues("illegal_event").Inc() // self metric
				continue
			}
			events = append(events, event)
		}
	}
	return events
}

func parseDogStatsDTagsToLabels(component string) map[string]string {
	labels := map[string]string{}
	//tagsReceived.Inc() // self metric
	tags := strings.Split(component, ",")
	for _, t := range tags {
		t = strings.TrimPrefix(t, "#")
		kv := strings.SplitN(t, ":", 2)

		if len(kv) < 2 || len(kv[1]) == 0 {
			//tagErrors.Inc() // self metric
			glog.V(10).Infof("Malformed or empty DogStatsD tag %s in component %s", t, component)
			continue
		}

		labels[metrics.EscapeMetricName(kv[0])] = kv[1]
	}
	return labels
}

func ipPortFromString(addr string) (*net.IPAddr, int) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		glog.Fatal("Bad StatsD listening address", addr)
	}

	if host == "" {
		host = "0.0.0.0"
	}
	ip, err := net.ResolveIPAddr("ip", host)
	if err != nil {
		glog.Fatalf("Unable to resolve %s: %s", host, err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		glog.Fatalf("Bad port %s: %s", portStr, err)
	}

	return ip, port
}

func udpAddrFromString(addr string) *net.UDPAddr {
	ip, port := ipPortFromString(addr)
	return &net.UDPAddr{
		IP:   ip.IP,
		Port: port,
		Zone: ip.Zone,
	}
}

func tcpAddrFromString(addr string) *net.TCPAddr {
	ip, port := ipPortFromString(addr)
	return &net.TCPAddr{
		IP:   ip.IP,
		Port: port,
		Zone: ip.Zone,
	}
}