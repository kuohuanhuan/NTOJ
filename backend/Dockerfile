FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /ntoj ./cmd/ntoj

FROM alpine:3.22
RUN adduser -D -u 10001 ntoj && mkdir /data && chown ntoj /data
USER ntoj
COPY --from=build /ntoj /usr/local/bin/ntoj
ENV DATA_DIR=/data LISTEN_ADDR=:8080
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s CMD wget -qO- http://127.0.0.1:8080/api/healthz || exit 1
ENTRYPOINT ["ntoj"]
