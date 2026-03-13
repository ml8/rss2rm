#!/bin/bash
# Build, push, and deploy rss2rm to GKE.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/config.env"

echo "=== Configuring Docker for Artifact Registry ==="
gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet

echo "=== Building container image ==="
docker buildx build --platform linux/amd64 -t "$IMAGE_NAME:latest" "$(dirname "$SCRIPT_DIR")"

echo "=== Pushing to Artifact Registry ==="
docker push "$IMAGE_NAME:latest"

echo "=== Getting Cloud SQL connection name ==="
CONNECTION_NAME=$(gcloud sql instances describe "$SQL_INSTANCE_NAME" \
  --format='value(connectionName)' --project="$PROJECT_ID")

echo "=== Resolving static IP ==="
STATIC_IP=$(gcloud compute addresses describe "$STATIC_IP_NAME" \
  --global --project="$PROJECT_ID" --format='value(address)')

echo "=== Applying Kubernetes manifests ==="
kubectl apply -n rss2rm -f "$SCRIPT_DIR/k8s/namespace.yaml"

export IMAGE_NAME CONNECTION_NAME DOMAIN STATIC_IP_NAME
envsubst < "$SCRIPT_DIR/k8s/deployment.yaml" | kubectl apply -n rss2rm -f -
envsubst < "$SCRIPT_DIR/k8s/service.yaml" | kubectl apply -n rss2rm -f -
kubectl apply -n rss2rm -f "$SCRIPT_DIR/k8s/admin-service.yaml"

echo "=== Restarting pods ==="
kubectl rollout restart deployment/rss2rm -n rss2rm

echo "=== Waiting for rollout ==="
kubectl rollout status deployment/rss2rm -n rss2rm --timeout=300s

STATIC_IP=$(gcloud compute addresses describe "$STATIC_IP_NAME" \
  --global --project="$PROJECT_ID" --format='value(address)')

echo ""
echo "=== Deployment complete ==="
echo "Static IP: $STATIC_IP"
echo "URL: https://$DOMAIN"
echo ""
echo "Certificate provisioning may take 10-20 minutes."
echo "Check status: kubectl describe managedcertificate rss2rm-cert -n rss2rm"
echo ""
echo "Admin API (internal only):"
echo "  kubectl port-forward svc/rss2rm-admin -n rss2rm 9090:9090"
echo "  Then: curl http://localhost:9090/admin/"
