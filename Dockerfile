# DZ Manager container image, built from source:
#   docker build -t dz-manager .
# See docs/docker.md. The result is one static binary on a distroless base,
# running as an unprivileged user, with all state in /data.

FROM node:22-bookworm-slim AS web
WORKDIR /src
COPY web/package.json web/package-lock.json web/
RUN npm ci --prefix web --no-audit --no-fund
COPY web web
COPY internal/web internal/web
RUN npm --prefix web run build

FROM golang:1.27-bookworm AS go
ARG VERSION=docker
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
COPY --from=web /src/internal/web/dist internal/web/dist
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/dzm ./cmd/dzm
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/dzm /usr/local/bin/dzm
COPY --from=go --chown=nonroot:nonroot /out/data /data
COPY LICENSES /usr/share/dz-manager/LICENSES
ENV DZM_CONTAINER=1 \
    DZM_LISTEN=0.0.0.0:8080 \
    DZM_DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8080
USER nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s CMD ["/usr/local/bin/dzm", "health"]
ENTRYPOINT ["/usr/local/bin/dzm"]
CMD ["serve"]
