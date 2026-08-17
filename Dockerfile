FROM caddy:2.11.4-builder AS builder

COPY . /src
WORKDIR /src

RUN xcaddy build \
    --output /usr/bin/caddy \
    --with github.com/jrsearles/caddy-dev-local=/src

FROM caddy:2.11.4

COPY --from=builder /usr/bin/caddy /usr/bin/caddy

CMD ["caddy", "devlocal"]
