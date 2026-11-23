# The image of one agent for kagent. The build context is this folder (kagent/),
# and AGENT names the agent to build:
#
#   docker build --build-arg AGENT=support-triage -t <registry>/support-triage kagent/
#
# Every image has the same command, /usr/local/bin/agent, which the Agent's
# workload names: kagent wants an explicit command.

FROM golang:1.27-alpine AS build
ARG AGENT
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY internal ./internal
COPY agents ./agents
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    test -n "$AGENT" && go build -trimpath -ldflags="-s -w" -o /out/agent ./agents/$AGENT
# kagent's runtime library writes the agent's config, its skills and its data
# into these folders, as the user the image runs as.
RUN mkdir -p /rootfs/config /rootfs/skills /rootfs/plugins /rootfs/data/plugins

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build --chown=65532:65532 /rootfs/ /
COPY --from=build /out/agent /usr/local/bin/agent
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/agent"]
