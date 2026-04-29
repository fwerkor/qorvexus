FROM golang:1.23-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/qorvexus ./cmd/qorvexus

FROM golang:1.23-bookworm

ARG PLAYWRIGHT_VERSION=1.48.2

ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    NODE_PATH=/usr/local/lib/node_modules \
    QORVEXUS_SOURCE_ROOT=/workspace/qorvexus

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates tzdata nodejs npm git make wget \
  && npm install -g playwright@${PLAYWRIGHT_VERSION} \
  && npx playwright install --with-deps chromium \
  && npm cache clean --force \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace/qorvexus

COPY --from=builder /out/qorvexus /usr/local/bin/qorvexus
COPY . /workspace/qorvexus
COPY docker/entrypoint.sh /usr/local/bin/qorvexus-entrypoint

RUN chmod +x /usr/local/bin/qorvexus-entrypoint

VOLUME ["/data"]

EXPOSE 7788

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:7788/api/status >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/usr/local/bin/qorvexus-entrypoint"]
