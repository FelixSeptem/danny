ARG NODE_BASE_IMAGE=docker.1ms.run/library/node:22-alpine
ARG GO_BASE_IMAGE=docker.1ms.run/library/golang:1.26-alpine
ARG RUNTIME_BASE_IMAGE=docker.1ms.run/library/alpine:3.20

# Stage 1: build the React + TypeScript workbench from the pinned lockfile.
FROM ${NODE_BASE_IMAGE} AS workbench
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# Stage 2: build the Go service. Both storage drivers are pure Go
# (modernc.org/sqlite and pgx), so no C toolchain or libc is needed.
FROM ${GO_BASE_IMAGE} AS service
ARG GO_PROXY=https://goproxy.cn,https://proxy.golang.org,direct
ENV GOPROXY=${GO_PROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/danny ./cmd/personal-agent

# Stage 3: minimal runtime. No source tree, Node.js, Python, Go, or C
# toolchain; the workbench assets and static binary are the only payloads.
FROM ${RUNTIME_BASE_IMAGE} AS runtime
RUN addgroup -S danny && adduser -S -G danny danny \
    && mkdir -p /var/lib/danny/data /var/lib/danny/documents \
                /var/lib/danny/artifacts /var/lib/danny/staging \
                /var/lib/danny/parser-jobs /opt/danny/web/dist \
    && chown -R danny:danny /var/lib/danny /opt/danny
COPY --from=service /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=service /out/danny /usr/local/bin/danny
COPY --from=workbench /src/web/dist/ /opt/danny/web/dist/

ENV DANNY_DATA_DIR=/var/lib/danny/data \
    DANNY_DOCUMENTS_DIR=/var/lib/danny/documents \
    DANNY_ARTIFACTS_DIR=/var/lib/danny/artifacts \
    DANNY_PARSER_STAGING_DIR=/var/lib/danny/staging \
    DANNY_PARSER_JOBS_DIR=/var/lib/danny/parser-jobs \
    DANNY_WORKBENCH_DIR=/opt/danny/web/dist \
    DANNY_DATABASE_URL=/var/lib/danny/data/danny.db \
    DANNY_LISTEN_ADDRESS=0.0.0.0:8080 \
    DANNY_CONTAINER_LISTEN=true

USER danny
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/danny"]
