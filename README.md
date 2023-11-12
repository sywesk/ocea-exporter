# OCEA Exporter

ocea-exporter is a tool that exports fluid consumption (hot water, cold water, heating) from meters installed by the company OCEA SB, as they do not provide customers with consumption graphs. Its goal is also to enable individuals to track their consumptions through home assistant.

It currently only supports prometheus, but will soon support home assistant by using MQTT & discovery.

## Configuration

The configuration is a short YAML file. Here's the reference (with the default values) :

```yaml
username: 
password: 
poll_interval: 30m
state_file_path: 
prometheus: 
  enabled: false
  listen_addr: 127.0.0.1:9001
home_assistant:
  enabled: false
  broker_addr: 
  username: 
  password: 
```

Note: `poll_interval` is a `time.Duration` string.

Environment variables can also be used to override the configuration. Add the prefix `OCEA_EXPORTER_` before the configuration key to get the corresponding environment variable. For example, `home_assistant.enabled` can be changed using the `OCEA_EXPORTER_HOME_ASSISTANT_ENABLED` environment variable.

## Installing

```
go install github.com/sywesk/ocea-exporter/cmd/ocea-exporter@latest
```

Or you can use the docker image `sywesk/ocea-exporter`.

## Running

```
ocea-exporter <path of your config file>
```

