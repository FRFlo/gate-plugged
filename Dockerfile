FROM --platform=$BUILDPLATFORM golang:1.26 AS build

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.sum ./

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY plugins ./plugins
COPY util ./util
COPY gate.go ./
COPY HackedServer/hackedserver-core/src/main/resources ./HackedServer/hackedserver-core/src/main/resources

# Automatically provided by the buildkit
ARG TARGETOS TARGETARCH

# Build
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -a -o gate gate.go

# Move binary into final image
# The Go build currently produces a dynamically linked executable (the
# embedded Gate dependencies use purego). Use the distroless base image so
# the required ELF loader is present at runtime.
FROM gcr.io/distroless/base-debian12 AS app
COPY --from=build /workspace/gate /
COPY --from=build /workspace/HackedServer/hackedserver-core/src/main/resources /HackedServer/hackedserver-core/src/main/resources
COPY config.yml /
COPY plugged.yml /
WORKDIR /
CMD ["/gate"]
