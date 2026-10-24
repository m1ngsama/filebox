# syntax=docker/dockerfile:1
# Build:  docker build -t filebox .                    (the small image)
#         docker build --target full -t filebox:full .  (adds Office previews and text-in-images search)
# Run:    see deploy/docker-compose.yml

FROM --platform=$BUILDPLATFORM oven/bun:1-alpine AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ENV GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist web/dist
ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG REVISION=
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.buildTag=$VERSION -X main.revision=$REVISION" -o /out/filebox ./cmd/filebox

FROM alpine:3 AS base
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

# Word, Excel and PowerPoint previews (LibreOffice, with Chinese, Japanese and Korean fonts) and `-ocr` (tesseract in
# English and Simplified Chinese). It is several times the size of the small image, so it is a separate tag.
FROM base AS full
USER root
RUN apk add --no-cache libreoffice-writer libreoffice-calc libreoffice-impress util-linux font-noto-cjk \
    tesseract-ocr tesseract-ocr-data-eng tesseract-ocr-data-chi_sim
USER filebox

# Last, so that a plain `docker build` makes this one.
FROM base AS slim
