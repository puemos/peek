# syntax=docker/dockerfile:1

# ---- Build stage ----
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

# Build the static server binary (pure-Go sqlite + embedded assets => no CGO).
COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w" -o /out/peekd ./cmd/peekd

# Prepare a nonroot passwd entry and a writable data dir owned by it.
RUN echo 'peek:x:65532:65532:peek:/:/sbin/nologin' > /out/passwd \
    && mkdir -p /out/data \
    && chown -R 65532:65532 /out/data

# ---- Final stage ----
FROM scratch

# CA certificates + nonroot user, copied from the build stage.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/passwd /etc/passwd

COPY --from=build /out/peekd /peekd
COPY --from=build --chown=65532:65532 /out/data /data

ENV PEEK_ADDR=:7700 \
    PEEK_DATA=/data

EXPOSE 7700
VOLUME ["/data"]
USER 65532:65532

ENTRYPOINT ["/peekd"]
