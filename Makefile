GO 		?= go
GOFMT   ?= $(GO)fmt
DOCKER  ?= docker

.PHONY: all
all: build container

.PHONY: build
build:
	@echo " 🦄 Mix sugar, spice, and everything nice with an accidental chemical X"
	$(GO) build

.PHONY: container
container: Dockerfile build
	@echo " 🐳 On its own little box"
	$(DOCKER) build -t jvosantos/statsd_exporter .

.PHONY: clean
clean: statsd_exporter

.PHONY: runcontainer
runcontainer: container
	docker run -it -p 8125:8125/udp -v `pwd`/mappings.yaml:/etc/statsd_exporter/mappings.yaml jvosantos/statsd_exporter:latest -mapping-config=/etc/statsd_exporter/mappings.yaml -statsd.listen-udp=:8125 -elasticsearch.url="https://73b2a6be8df7253c9eb5ae999c30fd33.eu-west-1.aws.found.io:9243" --elasticsearch.username=statsd-exporter -elasticsearch.password=3gfturneiTdgsRyzdJMd -v=1000 -logtostderr

.PHONY: run
run: build
	@echo " 🐠 Swimming around"
	./statsd_exporter -mapping-config=mappings.yaml -statsd.listen-udp=:8125 -elasticsearch.url="http://localhost:9200" -v=1000 -logtostderr
