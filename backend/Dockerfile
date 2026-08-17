# syntax=docker/dockerfile:1.7

FROM golang:1.26.5-alpine AS build

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

RUN apk add --no-cache build-base
CMD ["go", "test", "./backend/internal/platform/database", "./backend/internal/platform/workerstatus", "-race", "-v"]

FROM alpine:3.23.3 AS certificates

RUN apk add --no-cache ca-certificates

FROM alpine:3.23.3 AS runtime

ARG BINARY
RUN test "$BINARY" = api || test "$BINARY" = worker || test "$BINARY" = migrate \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app
COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=10001:10001 /out/${BINARY} /usr/local/bin/service

USER 10001:10001
ENTRYPOINT ["/usr/local/bin/service"]
