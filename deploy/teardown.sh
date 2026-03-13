#!/bin/bash
# Delete all rss2rm GCP resources.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/config.env"

echo "⚠  This will DELETE all rss2rm GCP resources:"
echo "   - GKE cluster: $CLUSTER_NAME"
echo "   - Cloud SQL instance: $SQL_INSTANCE_NAME (ALL DATA LOST)"
echo "   - Static IP: $STATIC_IP_NAME"
echo "   - Service account: rss2rm-sql"
echo ""
read -p "Type 'yes' to confirm: " confirm
[ "$confirm" = "yes" ] || { echo "Aborted."; exit 1; }

echo "=== Deleting Kubernetes namespace ==="
kubectl delete namespace rss2rm --ignore-not-found 2>/dev/null || true

echo "=== Deleting Cloud SQL instance ==="
gcloud sql instances delete "$SQL_INSTANCE_NAME" \
  --project="$PROJECT_ID" --quiet 2>/dev/null || true

echo "=== Deleting GKE cluster ==="
gcloud container clusters delete "$CLUSTER_NAME" \
  --region="$REGION" --project="$PROJECT_ID" --quiet

echo "=== Deleting service account ==="
gcloud iam service-accounts delete \
  "rss2rm-sql@${PROJECT_ID}.iam.gserviceaccount.com" \
  --project="$PROJECT_ID" --quiet 2>/dev/null || true

echo "=== Releasing static IP ==="
gcloud compute addresses delete "$STATIC_IP_NAME" \
  --global --project="$PROJECT_ID" --quiet 2>/dev/null || true

rm -f "$SCRIPT_DIR/.secrets.env"

echo "=== Teardown complete ==="
