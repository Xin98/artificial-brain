# syntax=docker/dockerfile:1.7

FROM golang:1.26.5-alpine AS build

# GOPROXY is overridable at build time for restricted networks (see
# docs/iterations/ITER-0002/decisions.md, D10); the default is upstream.
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY backend ./backend
COPY deploy/migrations ./deploy/migrations
RUN mkdir -p /out \
    && CGO_ENABLED=0 go build -trimpath -o /out/api ./backend/cmd/api \
    && CGO_ENABLED=0 go build -trimpath -o /out/worker ./backend/cmd/worker \
    && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./backend/cmd/migrate

FROM build AS test

# APK_MIRROR is overridable at build time for restricted networks (see
# docs/iterations/ITER-0002/decisions.md, D10); the default is upstream.
ARG APK_MIRROR=dl-cdn.alpinelinux.org
RUN sed -i "s|dl-cdn.alpinelinux.org|${APK_MIRROR}|g" /etc/apk/repositories \
    && apk add --no-cache build-base
CMD ["go", "test", "-p=1", "-race", "-v", "./backend/internal/platform/database", "./backend/internal/platform/workerstatus", "./backend/internal/modules/...", "./backend/cmd/api"]

FROM alpine:3.23.3 AS certificates

RUN apk add --no-cache ca-certificates

FROM alpine:3.23.3 AS runtime

# The IANA zoneinfo database is required: timezones travel per request (A1)
# and are validated/loaded at runtime.
RUN apk add --no-cache tzdata

ARG BINARY
RUN test "$BINARY" = api || test "$BINARY" = worker || test "$BINARY" = migrate \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app
COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=10001:10001 /out/${BINARY} /usr/local/bin/service

USER 10001:10001
ENTRYPOINT ["/usr/local/bin/service"]
