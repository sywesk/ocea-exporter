FROM golang:1.26.10-alpine AS builder

WORKDIR /app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /app/ocea-exporter ./cmd/ocea-exporter

FROM alpine:3.24

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/ocea-exporter ./

CMD ["./ocea-exporter", "./config.yaml"]
