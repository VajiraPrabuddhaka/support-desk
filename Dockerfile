# One image holds the five binaries: support-triage, billing-agent, console,
# seed and ask. Each service picks one with `command`.

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY agents ./agents
COPY console ./console
COPY internal ./internal
COPY tools ./tools
RUN go build -trimpath -ldflags="-s -w" -o /out/ ./agents/... ./console ./tools/... \
 && mkdir -p /out/secrets

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/support-triage /out/billing-agent /out/console /out/seed /out/ask /usr/local/bin/
# seed writes the bots' Gitea tokens here (a named volume); the directory
# must belong to the nonroot user before the volume is created.
COPY --from=build --chown=65532:65532 /out/secrets /secrets
USER nonroot:nonroot
