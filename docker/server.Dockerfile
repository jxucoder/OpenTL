# OpenTL Server Image
#
# Multi-stage build for the OpenTL server binary.
# The final image is a minimal scratch container.

FROM golang:1.23-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /opentl ./cmd/opentl

FROM alpine:3.20

RUN apk add --no-cache ca-certificates docker-cli

COPY --from=builder /opentl /usr/local/bin/opentl
RUN adduser -D -u 10001 opentl
USER opentl

ENTRYPOINT ["opentl", "serve"]
