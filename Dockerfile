# syntax=docker/dockerfile:1
# Build:  docker build -t filebox .
# Run:    see deploy/docker-compose.yml

FROM --platform=$BUILDPLATFORM oven/bun:1-alpine AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ENV GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist web/dist
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.buildTag=$VERSION" -o /out/filebox ./cmd/filebox

FROM alpine:3
# ffmpeg makes thumbnails and streams video; poppler-utils reads text out of PDFs for search.
RUN apk add --no-cache ffmpeg poppler-utils ca-certificates tzdata \
    && adduser -D -u 10001 filebox \
    && mkdir /data && chown filebox /data
COPY --from=build /out/filebox /usr/local/bin/filebox
USER filebox
VOLUME /data
EXPOSE 5280
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD wget -q -O /dev/null http://127.0.0.1:5280/ || exit 1
ENTRYPOINT ["filebox"]
CMD ["serve", "-data", "/data", "-listen", ":5280", "-ffmpeg", "/usr/bin/ffmpeg"]
