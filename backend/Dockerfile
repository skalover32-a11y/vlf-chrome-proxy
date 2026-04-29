FROM golang:1.24-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/socks5d ./cmd/socks5d
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/admin ./cmd/admin

FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /out/socks5d /app/socks5d
COPY --from=build /out/admin /app/admin

FROM runtime AS api
ENTRYPOINT ["/app/api"]

FROM runtime AS socks5d
ENTRYPOINT ["/app/socks5d"]

FROM runtime AS admin
ENTRYPOINT ["/app/admin"]
