# syntax=docker/dockerfile:1
# Build from repo root:
#   docker build -f control-plane/Dockerfile -t anchor-api .

FROM golang:1.26-bookworm AS build
WORKDIR /src/control-plane

COPY control-plane/go.mod control-plane/go.sum ./
RUN go mod download

COPY control-plane/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/control-plane ./cmd/server

FROM gcr.io/distroless/static-debian12
WORKDIR /app

COPY --from=build /out/control-plane /app/control-plane
COPY scripts/install.sh /app/scripts/install.sh
COPY release/ /app/release/

ENV ENV=production \
    DATABASE_PATH=/data/yourplatform.db \
    PORT=8080

EXPOSE 8080
# Root required so Render's attached disk at /data is writable for SQLite.
VOLUME ["/data"]

ENTRYPOINT ["/app/control-plane"]
