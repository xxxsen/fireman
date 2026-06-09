# Fireman backend (image name: fireman). Multi-stage build with CGO disabled
# so the resulting binary is statically linked against modernc.org/sqlite.

FROM golang:1.25-bookworm AS builder

WORKDIR /src

# Cache module downloads first.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOFLAGS=-trimpath

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -ldflags="-s -w" -o /out/fireman ./cmd/fireman

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/fireman /app/fireman
COPY docker/config.json /app/config.json

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/app/fireman"]
CMD ["run", "--config=/app/config.json"]
