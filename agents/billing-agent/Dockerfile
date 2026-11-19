# The image of billing-agent alone, which OpenChoreo builds from this
# repository (see openchoreo/agents/billing-agent.yaml). The build context is
# the root of the repository. Docker Compose uses the Dockerfile at the root,
# which holds every binary.

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY agents/billing-agent ./agents/billing-agent
RUN go build -trimpath -ldflags="-s -w" -o /out/billing-agent ./agents/billing-agent

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/billing-agent /usr/local/bin/billing-agent
USER nonroot:nonroot
# The workload descriptor has no command, so the image says what to run.
ENTRYPOINT ["/usr/local/bin/billing-agent"]
