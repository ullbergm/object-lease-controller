# Build stage
FROM node:24-alpine@sha256:4f696fbf39f383c1e486030ba6b289a5d9af541642fc78ab197e584a113b9c03 AS builder
WORKDIR /opt/app
COPY package.json tsconfig.json webpack.config.js ./
COPY src ./src
COPY public ./public
RUN yarn install --frozen-lockfile || yarn install \
 && yarn build

# Runtime stage: serve plugin assets over HTTPS with service certs
FROM registry.access.redhat.com/ubi9/nginx-120:latest@sha256:99d8a1d13835606114bb7785056793903c8739b7e1213b549f82ef30e0d51e5d
# Copy build
COPY --from=builder /opt/app/dist/ /opt/app-root/src/
COPY run.sh /usr/local/bin/run.sh
# Configure NGINX to serve static files over HTTPS
USER 0
RUN mkdir -p /opt/app-root/etc/nginx.d/ \
    && printf '%s\n' \
       'server {' \
       '  listen 8443 ssl;' \
       '  server_name _;' \
       '  ssl_certificate /var/serving-cert/tls.crt;' \
       '  ssl_certificate_key /var/serving-cert/tls.key;' \
       '  ssl_protocols TLSv1.2 TLSv1.3;' \
       '  root /opt/app-root/src;' \
       '  add_header X-Content-Type-Options nosniff always;' \
       '  location / {' \
       '    try_files $uri $uri/ /index.html;' \
       '  }' \
       '}' > /opt/app-root/etc/nginx.d/default.conf
# Service CA volume mount path
ENV PORT=8443
RUN chmod +x /usr/local/bin/run.sh
EXPOSE 8443
USER 1001
ENTRYPOINT ["/usr/local/bin/run.sh"]
CMD [""]
