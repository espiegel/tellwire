# syntax=docker/dockerfile:1
# Multi-stage build. Targets:  gateway (default, last stage) and mockmud.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/gateway ./cmd/gateway && \
    go build -trimpath -ldflags "-s -w" -o /out/mockmud ./cmd/mockmud

FROM gcr.io/distroless/static-debian12:nonroot AS mockmud
COPY --from=build /out/mockmud /mockmud
EXPOSE 2323
USER nonroot:nonroot
ENTRYPOINT ["/mockmud"]

FROM gcr.io/distroless/static-debian12:nonroot AS gateway
COPY --from=build /out/gateway /gateway
EXPOSE 8080
USER nonroot:nonroot
# distroless has no shell or curl: the binary has its own healthcheck subcommand.
HEALTHCHECK --interval=10s --timeout=4s --start-period=5s --retries=3 CMD ["/gateway", "healthcheck"]
ENTRYPOINT ["/gateway"]
