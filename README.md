# OCEA Exporter

OCEA exporter is a tool that exports fluid consumption (hot water, cold water, heating), as it does not provide customers with consumption graphs.

It currently only supports prometheus, but will soon support home assistant through MQTT.

## Configuration

The configuration is a short YAML file. Here's the reference:

```yaml
username: test@gmail.com
password: your_account_password
prometheus: 
  enable: true
  listen_addr: 127.0.0.1:9001
```

## How to start

```
go install github.com/sywesk/ocea-exporter/cmd/ocea-exporter@latest
ocea-exporter <your config file>
```