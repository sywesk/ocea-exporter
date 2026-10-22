# Contributing

Use Go 1.26.10 or newer. Tests use local HTTP/MQTT peers, generated Excel files,
and fake clocks/storage; production credentials are not required.

## Tests and checks

```sh
go test ./... -cover
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
go test -race ./...
go mod verify
go build ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 -scan package ./...
```

The race detector requires CGO and a C compiler. CI tests Linux and Windows,
runs the race detector and vulnerability scan on Linux, and builds the image.

Behavior tests cover owner backfill against the current inventory, occupant
exports, meter additions/replacements, invalid and partial readings, persistence
failures and retries, notification precision/isolation, metrics freshness,
configuration precedence, authentication/refresh, MQTT acknowledgements and
reconnection, and shutdown. Add a failing behavior test before fixing a bug.

An optional private capture regression can be run with `OCEA_TEST_HAR` pointing
to a local HAR that matches the test's export date. Captures and local credentials
must remain outside version control and the Docker build context.

## Dependency updates

Update the Go version and Docker builder together. The Excelize pseudo-version
is an intentional pin containing security fixes newer than stable 2.11.0; do not
downgrade it during a routine update. After changes, run both symbol and package
scans and the behavior suite.

At the current dependency versions, symbol and package scans find no affected
code or imported packages. A verbose scan reports GO-2026-5932 in the unused
`golang.org/x/crypto/openpgp` package. That package has no fix and is not imported;
Excelize uses other packages in `x/crypto`. Do not suppress findings in imported
packages or call paths.

## Local container build

```sh
docker compose up --build
```

The compose file builds the checkout, mounts `config.yaml` read-only, and stores
state in a named volume. Copy and customize `config.example.yaml` first. To
scrape Prometheus from outside the container, configure `prometheus.listen_addr`
as `0.0.0.0:9001` and publish that port in your deployment.

To tag a release, build the image with the chosen version and publish it through
the project's release process after the checks pass.
