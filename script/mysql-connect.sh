#!/bin/bash
# Connect to the rss2rm MySQL database via Cloud SQL Auth Proxy.
#
# Usage:
#   script/mysql-connect.sh                         # uses deploy/config.env
#   script/mysql-connect.sh -c path/to/config.env   # custom config
#   script/mysql-connect.sh -e "SELECT * FROM webhooks"  # run a query
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_ENV="$SCRIPT_DIR/../deploy/config.env"
NAMESPACE="rss2rm"
QUERY=""

while getopts "c:e:" opt; do
  case $opt in
    c) CONFIG_ENV="$OPTARG" ;;
    e) QUERY="$OPTARG" ;;
    *) echo "Usage: $0 [-c config.env] [-e 'SQL query']" >&2; exit 1 ;;
  esac
done

if [ ! -f "$CONFIG_ENV" ]; then
  echo "Error: config not found: $CONFIG_ENV" >&2
  exit 1
fi
source "$CONFIG_ENV"

# Fetch credentials from k8s secrets
echo "Fetching credentials from k8s secret rss2rm-db-secrets..."
DB_USER=$(kubectl get secret rss2rm-db-secrets -n "$NAMESPACE" -o jsonpath='{.data.username}' | base64 -d)
DB_PASS=$(kubectl get secret rss2rm-db-secrets -n "$NAMESPACE" -o jsonpath='{.data.password}' | base64 -d)
DB_NAME=$(kubectl get secret rss2rm-db-secrets -n "$NAMESPACE" -o jsonpath='{.data.database}' | base64 -d)

# Port-forward through the pod's Cloud SQL Auth Proxy sidecar
PROXY_PORT=13306
echo "Port-forwarding to rss2rm pod (Cloud SQL sidecar) on local port $PROXY_PORT..."
kubectl port-forward -n "$NAMESPACE" deployment/rss2rm "$PROXY_PORT":3306 &
PROXY_PID=$!
trap 'kill $PROXY_PID 2>/dev/null || true' EXIT

# Wait for port-forward to be ready
sleep 3
if ! kill -0 "$PROXY_PID" 2>/dev/null; then
  echo "Error: port-forward failed to start" >&2
  exit 1
fi

# Prefer mariadb client (handles mysql_native_password), fall back to mysql
MYSQL_CMD="mysql"
if command -v mariadb &>/dev/null; then
  MYSQL_CMD="mariadb"
fi

if [ -n "$QUERY" ]; then
  "$MYSQL_CMD" -h 127.0.0.1 -P "$PROXY_PORT" -u "$DB_USER" -p"$DB_PASS" "$DB_NAME" -e "$QUERY"
else
  echo "Connected. Use Ctrl-D to exit."
  "$MYSQL_CMD" -h 127.0.0.1 -P "$PROXY_PORT" -u "$DB_USER" -p"$DB_PASS" "$DB_NAME"
fi
