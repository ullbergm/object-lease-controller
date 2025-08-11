#!/usr/bin/env sh
set -eu

CERT_DIR="${CERT_DIR:-/var/serving-cert}"
CRT="$CERT_DIR/tls.crt"
KEY="$CERT_DIR/tls.key"
TIMEOUT="${STARTUP_TIMEOUT:-120}"

echo "[startup] Waiting for TLS certs in $CERT_DIR (timeout: ${TIMEOUT}s)"
COUNT=0
while [ "$COUNT" -lt "$TIMEOUT" ]; do
  if [ -s "$CRT" ] && [ -s "$KEY" ]; then
    echo "[startup] TLS certs found. Starting nginx..."
    exec nginx -g 'daemon off;'
  fi
  COUNT=$((COUNT + 1))
  sleep 1
done

echo "[startup] TLS certs not found in $CERT_DIR after ${TIMEOUT}s" >&2
exit 1
