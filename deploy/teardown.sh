#!/bin/bash
# Delete rss2rm GCP resources. Pass --delete-cluster to also delete the GKE cluster.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/config.env"

DELETE_CLUSTER=false
for arg in "$@"; do
  case "$arg" in
    --delete-cluster) DELETE_CLUSTER=true ;;
  esac
done

# Determine cluster location flag (zonal vs regional).
if [ -n "${CLUSTER_ZONE:-}" ]; then
  CLUSTER_LOC_FLAG="--zone=$CLUSTER_ZONE"
else
  CLUSTER_LOC_FLAG="--region=$REGION"
fi

echo "⚠  This will DELETE the following rss2rm GCP resources:"
echo "   - K8s namespace: rss2rm (all deployments, services, secrets)"
echo "   - Cloud SQL instance: $SQL_INSTANCE_NAME (ALL DATA LOST)"
echo "   - Artifact Registry repository: rss2rm"
echo "   - Static IP: $STATIC_IP_NAME"
echo "   - Service account: rss2rm-sql"
if [ "$DELETE_CLUSTER" = "true" ]; then
  echo "   - GKE cluster: $CLUSTER_NAME"
else
  echo "   (GKE cluster $CLUSTER_NAME will NOT be deleted. Pass --delete-cluster to include it.)"
fi
echo ""
read -p "Type 'yes' to confirm: " confirm
[ "$confirm" = "yes" ] || { echo "Aborted."; exit 1; }

echo "=== Deleting Kubernetes namespace ==="
kubectl delete namespace rss2rm --ignore-not-found 2>/dev/null || true

echo "=== Deleting Cloud SQL instance ==="
gcloud sql instances delete "$SQL_INSTANCE_NAME" \
  --project="$PROJECT_ID" --quiet 2>/dev/null || true

echo "=== Deleting Artifact Registry repository ==="
gcloud artifacts repositories delete rss2rm \
  --location="$REGION" --project="$PROJECT_ID" --quiet 2>/dev/null || true

if [ "$DELETE_CLUSTER" = "true" ]; then
  echo "=== Deleting GKE cluster ==="
  gcloud container clusters delete "$CLUSTER_NAME" \
    $CLUSTER_LOC_FLAG --project="$PROJECT_ID" --quiet
fi

echo "=== Deleting service account ==="
gcloud iam service-accounts delete \
  "rss2rm-sql@${PROJECT_ID}.iam.gserviceaccount.com" \
  --project="$PROJECT_ID" --quiet 2>/dev/null || true

echo "=== Releasing static IP ==="
gcloud compute addresses delete "$STATIC_IP_NAME" \
  --global --project="$PROJECT_ID" --quiet 2>/dev/null || true

rm -f "$SCRIPT_DIR/.secrets.env"

echo "=== Teardown complete ==="
