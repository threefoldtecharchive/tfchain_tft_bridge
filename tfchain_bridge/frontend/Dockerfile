FROM node:14 as builder
WORKDIR /app
ADD . .
RUN yarn
RUN yarn build

FROM caddy

COPY Caddyfile /etc/caddy/Caddyfile
COPY --from=builder /app/build /usr/share/caddy/