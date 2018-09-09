# STEP 1 build executable binary
FROM golang:alpine as builder
MAINTAINER Vasco Santos <jvosantos@gmail.com>

# Install SSL ca certificates
RUN apk update && apk add git && apk add ca-certificates

# Create application user
RUN adduser -D -g '' statsd_exporter

COPY . /go/src/github.com/jvosantos/statsd_exporter/
WORKDIR /go/src/github.com/jvosantos/statsd_exporter/

#get dependancies
RUN go get -d -v

#build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o /go/bin/statsd_exporter

# STEP 2 build a small image
# start from scratch
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd

# Copy the static executable
COPY --from=builder /go/bin/statsd_exporter /go/bin/statsd_exporter
USER statsd_exporter

ENTRYPOINT ["/go/bin/statsd_exporter"]

