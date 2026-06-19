FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /peekd ./cmd/peekd

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /peekd /usr/local/bin/peekd
VOLUME ["/data"]
EXPOSE 7700
ENTRYPOINT ["peekd"]
CMD ["--addr", ":7700", "--data", "/data", "--base-url", "http://localhost:7700"]
