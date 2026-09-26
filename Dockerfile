# syntax=docker/dockerfile:1

FROM golang:1-alpine AS build
WORKDIR /src

# Cached separately from the source: this layer only reruns when deps change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/godrop-server ./cmd/server

FROM alpine:3.20

# ca-certificates: needed to fetch https:// URLs. wget: used by HEALTHCHECK below.
RUN apk add --no-cache ca-certificates wget && \
    addgroup -S godrop && adduser -S -G godrop godrop

COPY --from=build /out/godrop-server /usr/local/bin/godrop-server

USER godrop
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/godrop-server"]
