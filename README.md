`statsd_exporter` receives StatsD-style metrics and exports them to an elasticsearch index.

## Overview

### With StatsD

To pipe metrics from an existing StatsD environment into Prometheus, configure
StatsD's repeater backend to repeat all received metrics to a `statsd_exporter`
process. This exporter translates StatsD metrics to Prometheus metrics via
configured mapping rules.

    +----------+                         +-------------------+                            +----------------+
    |  StatsD  |---(UDP/TCP repeater)--->|  statsd_exporter  |---(dimensional metrics)--->|  ElasticSearch |
    +----------+                         +-------------------+                            +----------------+

### Without StatsD

Since the StatsD exporter uses the same line protocol as StatsD itself, you can
also configure your applications to send StatsD metrics directly to the exporter.
In that case, you don't need to run a StatsD server anymore.

### DogStatsD extensions

The exporter will convert DogStatsD-style tags to prometheus labels. See
[Tags](http://docs.datadoghq.com/guides/dogstatsd/#tags) in the DogStatsD
documentation for the concept description and
[Datagram Format](http://docs.datadoghq.com/guides/dogstatsd/#datagram-format)
for specifics. It boils down to appending
`|#tag:value,another_tag:another_value` to the normal StatsD format.  Tags
without values (`#some_tag`) are not supported.

## Building and Running

    $ go build
    $ ./statsd_exporter --help

## Tests

    $ go test

## Metric Mapping and Configuration

The `statsd_exporter` can be configured to translate specific dot-separated StatsD
metrics into labeled Prometheus metrics via a simple mapping language. A
mapping definition starts with a line matching the StatsD metric in question,
with `*`s acting as wildcards for each dot-separated metric component. The
lines following the matching expression must contain one `label="value"` pair
each, and at least define the metric name (label name `name`). The Prometheus
metric is then constructed from these labels. `$n`-style references in the
label value are replaced by the n-th wildcard match in the matching line,
starting at 1. Multiple matching definitions are separated by one or more empty
lines. The first mapping rule that matches a StatsD metric wins.

Metrics that don't match any mapping in the configuration file are translated
into Prometheus metrics without any labels and with any non-alphanumeric
characters, including periods, translated into underscores.

```yaml
mappings:
- match: test.dispatcher.*.*.*
  name: "dispatcher_events_total"
  labels:
    processor: "$1"
    action: "$2"
    outcome: "$3"
    job: "test_dispatcher"
- match: *.signup.*.*
  name: "signup_events_total"
  labels:
    provider: "$2"
    outcome: "$3"
    job: "${1}_server"
```

This would transform these example StatsD metrics into Prometheus metrics as
follows:

    test.dispatcher.FooProcessor.send.success
     => dispatcher_events_total{processor="FooProcessor", action="send", outcome="success", job="test_dispatcher"}

    foo_product.signup.facebook.failure
     => signup_events_total{provider="facebook", outcome="failure", job="foo_product_server"}

    test.web-server.foo.bar
     => test_web_server_foo_bar{}

Each mapping in the configuration file must define a `name` for the metric. The
metric's name can contain `$n`-style references to be replaced by the n-th
wildcard match in the matching line. That allows for dynamic rewrites, such as:

```yaml
mappings:
- match: test.*.*.counter
  name: "${2}_total"
  labels:
    provider: "$1"
```

The metric name can also contain references to regex matches. The mapping above
could be written as:

```
mappings:
- match: test\.(\w+)\.(\w+)\.counter
  match_type: regex
  name: "${2}_total"
  labels:
    provider: "$1"
```

Please note that metrics with the same name must also have the same set of
label names.

If the default metric help text is insufficient for your needs you may use the YAML
configuration to specify a custom help text for each mapping:

```yaml
mappings:
- match: http.request.*
  help: "Total number of http requests"
  name: "http_requests_total"
  labels:
    code: "$1"
```

In the configuration, one may also set the timer type to "histogram". The
default is "summary" as in the plain text configuration format.  For example,
to set the timer type for a single metric:

```yaml
mappings:
- match: test.timing.*.*.*
  timer_type: raw_timer
  buckets: [ 0.01, 0.025, 0.05, 0.1 ]
  name: "my_timer"
  labels:
    provider: "$2"
    outcome: "$3"
    job: "${1}_server"
```

Another capability when using YAML configuration is the ability to define matches
using raw regular expressions as opposed to the default globbing style of match.
This may allow for pulling structured data from otherwise poorly named statsd
metrics AND allow for more precise targetting of match rules. When no `match_type`
paramter is specified the default value of `glob` will be assumed:

```yaml
mappings:
- match: (.*)\.(.*)--(.*)\.status\.(.*)\.count
  match_type: regex
  name: "request_total"
  labels:
    hostname: "$1"
    exec: "$2"
    protocol: "$3"
    code: "$4"
```

`timer_type` is only used when the statsd metric type is a timer. `buckets` is
only used when the statsd metric type is a timerand the `timer_type` is set to
"histogram."

One may also set defaults for the timer type, buckets and match_type. These will be used
by all mappings that do not define these.

```yaml
defaults:
  timer_type: histogram
  buckets: [.005, .01, .025, .05, .1, .25, .5, 1, 2.5 ]
  match_type: glob
mappings:
# This will be a histogram using the buckets set in `defaults`.
- match: test.timing.*.*.*
  name: "my_timer"
  labels:
    provider: "$2"
    outcome: "$3"
    job: "${1}_server"
# This will be a summary timer.
- match: other.timing.*.*.*
  timer_type: summary
  name: "other_timer"
  labels:
    provider: "$2"
    outcome: "$3"
    job: "${1}_server_other"
```

You may also drop metrics by specifying a "drop" action on a match. For example:

```yaml
mappings:
# This metric would match as normal.
- match: test.timing.*.*.*
  name: "my_timer"
  labels:
    provider: "$2"
    outcome: "$3"
    job: "${1}_server"
# Any metric not matched will be dropped because "." matches all metrics.
- match: .
  match_type: regex
  action: drop
  name: "dropped"
```

You can drop any metric using the normal match syntax.
The default action is "map" which does the normal metrics mapping.

StatsD allows emitting of different metric types under the same metric name,
but the Prometheus client library can't merge those. For this use-case the
mapping definition allows you to specify which metric type to match:

```
mappings:
- match: test.foo.*
  name: "test_foo"
  match_metric_type: counter
  labels:
    provider: "$1"
```

Possible values for `match_metric_type` are `gauge`, `counter` and `timer`.

