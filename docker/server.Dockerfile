# TeleCoder Server Image
#
# Multi-stage build for the TeleCoder server binary.
# The final image is a minimal scratch container.

FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /telecoder ./cmd/telecoder

FROM alpine:3.20

RUN apk add --no-cache ca-certificates docker-cli

COPY --from=builder /telecoder /usr/local/bin/telecoder
RUN adduser -D -u 10001 telecoder
USER telecoder

ENTRYPOINT ["telecoder", "serve"]
