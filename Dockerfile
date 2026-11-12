FROM --platform=$BUILDPLATFORM node:24-alpine AS webui
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS build

ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY api ./api
COPY app/webui/assets.go ./app/webui/
COPY --from=webui /src/app/webui/dist ./app/webui/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/pandaopds ./cmd/pandaopds
RUN mkdir -p /out/config/archive /out/data/cache /out/data/archives && chown -R 65532:65532 /out/config /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /srv/pandaopds
COPY --from=build /out/pandaopds /usr/local/bin/pandaopds
COPY --from=build /out/config /config
COPY --from=build /out/data /data
# Preserve the existing Python container's root default so root-owned named
# volumes from earlier deployments remain writable without a migration.
USER 0:0

ENV SETTINGS_CONFIG=/config/settings.toml \
    HOME_CONFIG=/config/home.toml \
    HTML_INTERVAL_SECONDS=1.5 \
    MAX_CONCURRENCY=2 \
    IMAGE_MAX_CONCURRENCY=5 \
    THUMB_MAX_CONCURRENCY=25 \
    CACHE_DIR=/data/cache \
    ARCHIVE_DIR=/data/archives \
    ARCHIVE_STATE_DIR=/config/archive \
    PORT=8000

EXPOSE 8000
ENTRYPOINT ["/usr/local/bin/pandaopds"]
