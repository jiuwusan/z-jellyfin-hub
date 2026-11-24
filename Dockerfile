FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=web /src/internal/httpapi/webassets/dist ./internal/httpapi/webassets/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/hub ./cmd/hub

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata && addgroup -g 10001 hub && adduser -D -u 10001 -G hub hub \
    && mkdir -p /data && chown 10001:10001 /data
COPY --from=build /out/hub /usr/local/bin/hub
USER 10001:10001
ENV HUB_DATA_DIR=/data HUB_LISTEN=:8080
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["hub", "health"]
ENTRYPOINT ["hub"]
CMD ["serve"]
