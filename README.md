# OCEA Exporter

ocea-exporter is a tool that exports fluid consumption (hot water, cold water, heating) from meters installed by the company OCEA SB, as they do not provide customers with consumption graphs. Its goal is also to enable individuals to track their consumptions through home assistant.

It currently only supports prometheus, but will soon support home assistant by using MQTT & discovery.

## Configuration

The configuration is a short YAML file. Here's the reference (with the default values) :

```yaml
username: 
password: 
poll_interval: 30m
prometheus: 
  enabled: false
  listen_addr: 127.0.0.1:9001
```

Note: `poll_interval` is a `time.Duration` string.

## Installing

```
go install github.com/sywesk/ocea-exporter/cmd/ocea-exporter@latest
```

## Running

```
ocea-exporter <path of your config file>
```