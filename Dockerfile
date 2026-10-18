FROM --platform=$BUILDPLATFORM golang:1.23.9-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/tuya-ipc-terminal .

FROM alpine:3.22
LABEL org.opencontainers.image.source="https://github.com/sanderio-ops/tuya-ipc-terminal"
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app \
    && mkdir -p /data && chown 10001:10001 /data
COPY --from=build /out/tuya-ipc-terminal /usr/local/bin/tuya-ipc-terminal
COPY --chmod=0555 entrypoint.sh /usr/local/bin/tuya-ipc-entrypoint
USER 10001:10001
WORKDIR /data
EXPOSE 8554
ENTRYPOINT ["/usr/local/bin/tuya-ipc-entrypoint"]
CMD ["rtsp", "start", "--port", "8554"]
