FROM golang:1.23-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/qorvexus ./cmd/qorvexus

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/qorvexus /usr/local/bin/qorvexus
COPY docker/entrypoint.sh /usr/local/bin/qorvexus-entrypoint

RUN chmod +x /usr/local/bin/qorvexus-entrypoint

VOLUME ["/data"]

EXPOSE 7788

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:7788/api/status >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/usr/local/bin/qorvexus-entrypoint"]
