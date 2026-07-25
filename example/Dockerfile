FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /caddy-dev-local .

FROM caddy:2-alpine

COPY --from=builder /caddy-dev-local /usr/bin/caddy-dev-local

ENTRYPOINT ["caddy-dev-local"]
