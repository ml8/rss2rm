#!/bin/bash
# One-time setup: create GKE cluster, Cloud SQL instance, service accounts.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/config.env"

# Determine cluster location flag (zonal vs regional).
if [ -n "${CLUSTER_ZONE:-}" ]; then
  CLUSTER_LOC_FLAG="--zone=$CLUSTER_ZONE"
else
  CLUSTER_LOC_FLAG="--region=$REGION"
fi

echo "=== Enabling GCP APIs ==="
gcloud services enable \
  container.googleapis.com \
  sqladmin.googleapis.com \
  artifactregistry.googleapis.com \
  --project="$PROJECT_ID"

echo "=== Creating Artifact Registry repository ==="
gcloud artifacts repositories create rss2rm \
  --repository-format=docker \
  --location="$REGION" \
  --project="$PROJECT_ID" 2>/dev/null || true

echo "=== Reserving global static IP ==="
gcloud compute addresses create "$STATIC_IP_NAME" \
  --global \
  --project="$PROJECT_ID" 2>/dev/null || true
STATIC_IP=$(gcloud compute addresses describe "$STATIC_IP_NAME" \
  --global --project="$PROJECT_ID" --format='value(address)')
echo "Static IP: $STATIC_IP"
echo "Point your DNS A record: $DOMAIN → $STATIC_IP"

echo "=== Creating GKE cluster (skipped if it already exists) ==="
gcloud container clusters create-auto "$CLUSTER_NAME" \
  $CLUSTER_LOC_FLAG \
  --project="$PROJECT_ID" 2>/dev/null || true

echo "=== Getting cluster credentials ==="
gcloud container clusters get-credentials "$CLUSTER_NAME" \
  $CLUSTER_LOC_FLAG \
  --project="$PROJECT_ID"

echo "=== Creating Cloud SQL instance (this takes a few minutes) ==="
gcloud sql instances create "$SQL_INSTANCE_NAME" \
  --database-version=MYSQL_8_0 \
  --tier=db-f1-micro \
  --region="$REGION" \
  --project="$PROJECT_ID" \
  --no-assign-ip \
  --network=default

echo "=== Creating database ==="
gcloud sql databases create "$SQL_DB_NAME" \
  --instance="$SQL_INSTANCE_NAME" \
  --project="$PROJECT_ID"

echo "=== Creating database user ==="
DB_PASSWORD=$(openssl rand -base64 24)
gcloud sql users create "$SQL_USER" \
  --instance="$SQL_INSTANCE_NAME" \
  --password="$DB_PASSWORD" \
  --project="$PROJECT_ID"

echo "DB_PASSWORD=$DB_PASSWORD" > "$SCRIPT_DIR/.secrets.env"
echo "⚠  Database password saved to deploy/.secrets.env — do NOT commit this file."

echo "=== Creating Google Service Account for Cloud SQL ==="
GSA_NAME=rss2rm-sql
gcloud iam service-accounts create "$GSA_NAME" \
  --display-name="rss2rm Cloud SQL client" \
  --project="$PROJECT_ID" 2>/dev/null || true

gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:${GSA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role="roles/cloudsql.client" \
  --condition=None \
  --quiet

echo "=== Configuring Workload Identity ==="
kubectl create namespace rss2rm 2>/dev/null || true
kubectl create serviceaccount rss2rm -n rss2rm 2>/dev/null || true

gcloud iam service-accounts add-iam-policy-binding \
  "${GSA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role="roles/iam.workloadIdentityUser" \
  --member="serviceAccount:${PROJECT_ID}.svc.id.goog[rss2rm/rss2rm]" \
  --condition=None \
  --quiet

kubectl annotate serviceaccount rss2rm \
  --namespace=rss2rm \
  --overwrite \
  "iam.gke.io/gcp-service-account=${GSA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

echo "=== Creating Kubernetes secrets ==="
kubectl create secret generic rss2rm-db-secrets \
  --namespace=rss2rm \
  --from-literal=username="$SQL_USER" \
  --from-literal=password="$DB_PASSWORD" \
  --from-literal=database="$SQL_DB_NAME" \
  --dry-run=client -o yaml | kubectl apply -f -

ADMIN_TOKEN=$(openssl rand -base64 32)
kubectl create secret generic rss2rm-admin-secrets \
  --namespace=rss2rm \
  --from-literal=token="$ADMIN_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "ADMIN_TOKEN=$ADMIN_TOKEN" >> "$SCRIPT_DIR/.secrets.env"

CONNECTION_NAME=$(gcloud sql instances describe "$SQL_INSTANCE_NAME" \
  --format='value(connectionName)' --project="$PROJECT_ID")

echo ""
echo "=== Setup complete ==="
echo "Static IP: $STATIC_IP"
echo "Cloud SQL connection: $CONNECTION_NAME"
echo "Admin token: $ADMIN_TOKEN"
echo ""
echo "DNS: Create an A record: $DOMAIN → $STATIC_IP"
echo "Certificate provisioning requires DNS to be configured before deploying."
echo "Next: configure DNS, then run deploy/deploy.sh"
