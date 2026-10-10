# syntax=docker/dockerfile:1

# Build stage: full Go toolchain, discarded afterwards.
FROM golang:1.27 AS build
WORKDIR /src

# Download modules first so this layer is cached while only code changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
# CGO_ENABLED=0 yields a static binary that runs without libc.
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/upwatch ./cmd/upwatch
# The runtime image has no shell for mkdir. Create the data directory here
# and copy it with the right owner, see the COPY below.
RUN mkdir /out/data

# Runtime stage: no shell, no package manager, runs as non-root.
# Contains only CA certificates, tzdata and our binary.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/upwatch /upwatch
# Without this, Docker creates /data for the volume as root, and the
# nonroot process cannot create its database there.
COPY --from=build --chown=nonroot:nonroot /out/data /data

ENV UPWATCH_ADDR=:8080 \
    UPWATCH_DB=/data/upwatch.db \
    UPWATCH_LOG_JSON=true
VOLUME /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/upwatch"]
