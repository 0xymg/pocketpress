# syntax=docker/dockerfile:1
#
# PocketPress multi-stage build.
#
# Stage 1 builds the React admin SPA. Stage 2 embeds that build into a
# static, CGO-free Go binary. The final stage ships only the binary on a
# minimal Alpine base with a non-root user.
#
# Usage:
#   docker build -t pocketpress/pocketpress .
#   docker run -p 8090:8090 -v pocketpress_data:/app/data pocketpress/pocketpress

# ---- Stage 1: build the admin SPA ----
FROM node:22-alpine AS admin-builder
WORKDIR /admin

# Install dependencies from the lockfile first so this layer caches until
# the lockfile changes.
COPY admin/package.json admin/package-lock.json ./
RUN npm ci

# Copy the rest of the admin sources and build. .dockerignore keeps
# node_modules and any pre-existing dist/ out of this copy.
COPY admin/ ./
RUN npm run build

# ---- Stage 2: build the Go binary with the admin build embedded ----
FROM golang:1.26-alpine AS backend-builder
WORKDIR /src

# Download modules first for layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy the backend sources, then drop the freshly built admin output into
# admin/dist so the //go:embed all:dist directive picks up the real SPA.
COPY . .
COPY --from=admin-builder /admin/dist ./admin/dist

# VERSION is stamped into the binary via -ldflags. Pass with
# `docker build --build-arg VERSION=x.y.z`.
ARG VERSION=0.1.0
RUN CGO_ENABLED=0 GOOS=linux go build \
	-ldflags "-s -w -X github.com/pocketpress/pocketpress/internal/app.Version=${VERSION}" \
	-o /out/pocketpress ./cmd/pocketpress

# ---- Stage 3: minimal runtime ----
FROM alpine:3 AS runtime

# ca-certificates for outbound HTTPS (webhooks, S3) in later phases.
# Create a dedicated non-root user and a data directory it can write to.
RUN apk add --no-cache ca-certificates \
	&& addgroup -S pocketpress \
	&& adduser -S -G pocketpress -h /app pocketpress \
	&& mkdir -p /app/data \
	&& chown -R pocketpress:pocketpress /app

COPY --from=backend-builder /out/pocketpress /usr/local/bin/pocketpress

ENV PP_ENV=production \
	PP_DATA_DIR=/app/data \
	PP_HTTP_ADDR=0.0.0.0:8090

USER pocketpress
WORKDIR /app

# Persist the SQLite database and local uploads.
VOLUME /app/data
EXPOSE 8090

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
	CMD wget --quiet --spider http://127.0.0.1:8090/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/pocketpress"]
CMD ["serve"]
