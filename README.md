# OCEA Exporter

ocea-exporter is a tool that exports fluid consumption (hot water, cold water, heating) from meters installed by the company OCEA SB, as they do not provide customers with consumption graphs. Its goal is also to enable individuals to track their consumption through home assistant.

It currently supports prometheus and home assistant by using MQTT & auto-discovery.

## Configuration

The configuration is a short YAML file. Here's the reference (with the default values):

```yaml
username: <username>
password: <password>
poll_interval: 30m
state_file_path: 
prometheus: 
  enabled: true
  listen_addr: 127.0.0.1:9001
home_assistant:
  enabled: true
  broker_addr: <broker ip address>:1883
  username: <broker username>
  password: <broker password>
debug: false
```

Note: `poll_interval` is a `time.Duration` string, so you can use `1h`, `10m`, `1d`, or `1h30m`, and it will do what you think it does.

Environment variables can also be used to override the configuration. Add the prefix `OCEA_EXPORTER_` before the configuration key to get the corresponding environment variable. For example, `home_assistant.enabled` can be changed using the `OCEA_EXPORTER_HOME_ASSISTANT_ENABLED` environment variable.

## Installing

```sh
go install github.com/sywesk/ocea-exporter/cmd/ocea-exporter@latest
```

Or you can use the docker image `sywesk/ocea-exporter`.

## Running

```sh
ocea-exporter <path of your config file>
```

### Example docker-compose file

```yaml
version: '3'

services:
  ocea_exporter:
    container_name: ocea-exporter
    image: sywesk/ocea-exporter:v0.5.4
    restart: always
    command: "/app/ocea-exporter"
    environment:
      OCEA_EXPORTER_USERNAME: "<put your creds here>"
      OCEA_EXPORTER_PASSWORD: "<put your creds here>"
      OCEA_EXPORTER_STATE_FILE_PATH: "/data/state.json"
      OCEA_EXPORTER_HOME_ASSISTANT_ENABLED: "true"
      OCEA_EXPORTER_HOME_ASSISTANT_BROKER_ADDR: "192.168.1.53:1883"
      OCEA_EXPORTER_HOME_ASSISTANT_USERNAME: "ocea-exporter"
      OCEA_EXPORTER_HOME_ASSISTANT_PASSWORD: "<put your creds here>"
    volumes:
      - /opt/ocea-exporter:/data
```

## Changelog

### 0.5.4

- Make the MQTT integration add the default 1883 port and output a warning.
- Make the README show how to set the port & move the changelog at the bottom.
- Stop warning messages from outputting stacktraces.

### 0.5.3

Add back the sensor name to make the entity naming clearer.

### 0.5.2

Change the way device names work, taking advantage of the MQTT topic breaking change.

### 0.5.1

This release adds the cleanup code to remove the old MQTT topics.

### 0.5.0

This release adds support for multiple meters per fluid. Typically, for some buildings, there are multiple cold/hot water vertical lines, and each one needs a separate meter (think a meter for the bathroom and one for the kitchen). This wasn't supported until this release.

__BREAKING CHANGE__: MQTT topics have been renamed to support multiple meters for the same fluid.