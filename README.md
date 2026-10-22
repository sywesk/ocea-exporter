# OCEA Exporter

ocea-exporter is a tool that exports fluid consumption (hot water, cold water, heating) from meters installed by the company OCEA SB, as they do not provide customers with consumption graphs. Its goal is also to enable individuals to track their consumption through home assistant.

It currently supports prometheus and home assistant by using MQTT & auto-discovery.

For an owner (`Proprietaire`) account, the meter-reading flow discovers the
meters through `GET /local/{id}/appareils`, requests a dated token through
`GET /local/{id}/token`, then sends `localId`, `token`, and `pdsIds` to
`POST /local/demande`. Missing readings for today are back-filled from yesterday,
while keeping today's readings when available. The current meter inventory is
used to distinguish missing readings from retired or replaced meters. A poll
is published only when every current meter has a valid reading and the complete
snapshot has been saved successfully.
The login-page parser accepts whitespace around its embedded settings fields.

Occupant (`Occupant`) accounts are detected from the resident's occupation and
use the period-based Excel export at `POST /local/demande/export`. The exporter
requests yesterday through today and keeps the latest available index per meter.
Empty indexes are skipped, while actual zero readings are preserved. Worksheet
meter serials are matched to the API's device IDs, and fluid names are normalized
to the same values used by owner accounts. Excel dates have daily precision.

## Configuration

The configuration is a short YAML file. The defaults are shown below; credentials must be supplied. Enable the integrations you want to use.

```yaml
username: <username>
password: <password>
poll_interval: 3h
state_file_path: 
prometheus: 
  enabled: false
  listen_addr: 127.0.0.1:9001
home_assistant:
  enabled: false
  broker_addr: <broker ip address>:1883
  username: <broker username>
  password: <broker password>
debug: false
```

`poll_interval` must be a positive Go duration, such as `1h`, `10m`, `24h`, or
`1h30m`. The `d` suffix is not supported. Unknown configuration keys, invalid
booleans, and invalid broker addresses are rejected at startup. Integration
flags default to `false`; an explicit `false` is preserved.

When omitted, `state_file_path` is `ocea-exporter/state.json` under the OS user
configuration directory. Its parent directories are created with private
permissions. Snapshots are written to a temporary file, synced, then renamed over
the previous snapshot. A corrupt state file causes a clear startup error; move
it aside to rebuild state from the portal.

MQTT accepts a hostname or IP with an optional port (default `1883`), IPv6, and
explicit `tcp://`, `ssl://`, `tls://`, `ws://`, or `wss://` URLs. TLS defaults to
port `8883`. Discovery and values use retained QoS 1 messages, checked for
acknowledgement. Failed connections and publications are retried with the latest
snapshot. Discovery is refreshed on successful polls, and meters retired during
the process lifetime have their retained discovery and state topics removed.

Environment variables can also be used to override the configuration. Add the prefix `OCEA_EXPORTER_` before the configuration key to get the corresponding environment variable. For example, `home_assistant.enabled` can be changed using the `OCEA_EXPORTER_HOME_ASSISTANT_ENABLED` environment variable.

## Installing

Building from source requires Go **1.26.10 or newer**. Go versions supporting
automatic toolchain downloads will select the version required by `go.mod`.

```sh
go install github.com/sywesk/ocea-exporter/cmd/ocea-exporter@latest
```

Or you can use the docker image `sywesk/ocea-exporter`.

## Running

```sh
ocea-exporter <path of your config file>
```

For a local owner-account check, copy `config.example.yaml` to `config.local.yaml`
and enter your credentials there. Then run:

```sh
go run ./cmd/ocea-exporter ./config.local.yaml
```

The example enables Prometheus at `http://127.0.0.1:9001/metrics` and disables MQTT.
`config.local.yaml` and `state.local.json` are ignored by Git. Use this separate
state file for testing so that an existing deployment's state is preserved.
For an occupant-account check, use `config.local.occupant.yaml` with
`state_file_path: ./state.local.occupant.json`. Both are also ignored by Git.

### Example docker-compose file

```yaml
services:
  ocea_exporter:
    container_name: ocea-exporter
    build: .
    image: ocea-exporter:local
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

## Metrics and lifecycle

Existing index series and Home Assistant discovery IDs remain unchanged:

- `ocea_metering_index{fluid,local_id}`: aggregate index per fluid.
- `ocea_metering_device_index{serial,fluid,local_id}`: index per current meter.
- `ocea_scrape_success`: `1` when the latest portal poll and persistence succeeded,
  otherwise `0`.
- `ocea_last_success_timestamp_seconds`: time of the last successful poll.
- `ocea_metering_reading_timestamp_seconds{serial,fluid,local_id}`: date of the
  actual source reading, when present. Dates without an offset are interpreted
  as UTC; occupant exports have daily precision.

A failed poll preserves the previous values and last-success timestamp. Monitor
success and reading age together: a working portal may still return yesterday's
reading. Retired meters and fluids disappear from the index metrics after the
next complete successful poll.

SIGINT/SIGTERM cancel portal requests and MQTT operations, stop the workers and
close the metrics server. An unavailable metrics listen address fails startup.

## Development and dependency updates

See [CONTRIBUTING.md](CONTRIBUTING.md) for tests and security checks. The Excelize
version in `go.mod` is intentionally pinned to an exact upstream commit: it
contains security fixes not yet included in the latest stable `2.11.0` release.
Recheck those fixes before replacing the pin with a stable release.

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
