FROM node:24-slim AS builder
WORKDIR /app
RUN npm install -g pnpm@12.16.1
COPY . .
RUN pnpm install --frozen-lockfile
ARG ADMIN_APP=platform
ARG FRONTEND_CONFIG_REVISION
RUN --mount=type=secret,id=frontend_aes_key,required=true \
    test -n "$FRONTEND_CONFIG_REVISION" && \
    case "$ADMIN_APP" in platform|merchant) ;; *) exit 1 ;; esac && \
    VITE_AES_KEY="$(cat /run/secrets/frontend_aes_key)" npm run "build:$ADMIN_APP"

FROM nginx:stable-alpine
ARG ADMIN_APP=platform
ENV ADMIN_APP=${ADMIN_APP}
ENV MERCHANT_CODE=""
ENV ADMIN_API_UPSTREAM=http://host.docker.internal:7788
COPY --from=builder /app/${ADMIN_APP}-admin/dist /usr/share/nginx/html
COPY scripts/deploy/admin.conf.template /etc/nginx/templates/default.conf.template
COPY scripts/deploy/15-admin-config.sh /docker-entrypoint.d/15-admin-config.envsh
RUN chmod +x /docker-entrypoint.d/15-admin-config.envsh
RUN printf '%s\n' "$ADMIN_APP" > /etc/admin-app
EXPOSE 8080
