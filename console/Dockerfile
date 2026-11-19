# The image of the console alone, which OpenChoreo builds from this repository
# (see openchoreo/app/desk-console.yaml). The build context is the root of the
# repository. Docker Compose uses the Dockerfile at the root, which holds every
# binary.

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY console ./console
RUN go build -trimpath -ldflags="-s -w" -o /out/console ./console

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/console /usr/local/bin/console
USER nonroot:nonroot
# The workload descriptor has no command, so the image says what to run.
ENTRYPOINT ["/usr/local/bin/console"]
