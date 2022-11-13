# OCEA Exporter

OCEA exporter is a tool that exports fluid consumption (hot water, cold water, heating) for prometheus, as it does not provide customers with consumption graphs.

## Configuration

The configuration is a short YAML file. Here's the reference:

```yaml
username: test@gmail.com
password: your_account_password
metrics_addr: 127.0.0.1:9001
```

## How to start

```
go install github.com/sywesk/ocea-exporter@latest
ocea-exporter <your config file>
```