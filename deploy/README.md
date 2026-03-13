# Deployment

Deploy rss2rm to GKE with Cloud SQL, HTTPS, and a custom domain.

## Architecture

```
Client (https://feeds.example.org)
  │
  ▼
Google Cloud Load Balancer (global static IP, managed TLS certificate)
  │
  ▼
GKE Pod
├── rss2rm (Go binary + web UI)
└── cloud-sql-auth-proxy (localhost:3306 → Cloud SQL)

Cloud SQL (MySQL 8.0, private IP)
```

All infrastructure is Google-managed: database backups and patching (Cloud SQL), and certificate renewal (ManagedCertificate). Autopilot clusters also manage node upgrades automatically.

## Prerequisites

1. [Google Cloud SDK](https://cloud.google.com/sdk/docs/install) installed and authenticated (`gcloud auth login`)
2. A GCP project with billing enabled
3. Docker installed locally
4. `kubectl` installed
5. `envsubst` available (part of `gettext`; pre-installed on most Linux/macOS)
6. A domain you control (for the DNS A record)

## Configuration

Edit `deploy/config.env`:

```bash
PROJECT_ID=your-gcp-project     # GCP project ID
REGION=us-central1               # Region for database, Artifact Registry, and cluster (if regional)
CLUSTER_NAME=rss2rm              # GKE cluster name
CLUSTER_ZONE=                    # Set for zonal clusters (e.g. us-central1-a). Leave empty for regional.
STATIC_IP_NAME=rss2rm-ip         # Name for the global static IP
DOMAIN=feeds.example.org         # Domain for HTTPS certificate
SQL_INSTANCE_NAME=rss2rm-db      # Cloud SQL instance name
SQL_DB_NAME=rss2rm               # MySQL database name
SQL_USER=rss2rm                  # MySQL username
IMAGE_NAME=${REGION}-docker.pkg.dev/${PROJECT_ID}/rss2rm/rss2rm
```

## Using an existing cluster

To deploy to a pre-existing GKE cluster instead of creating a new one:

1. Set `CLUSTER_NAME` to the name of your existing cluster.
2. If the cluster is **zonal**, set `CLUSTER_ZONE` to its zone (e.g., `us-west1-a`). Leave it empty for regional clusters.

`setup.sh` will skip cluster creation if the cluster already exists and fetch credentials for it. `teardown.sh` does not delete the cluster by default — pass `--delete-cluster` to include it.

## Step-by-step

### 1. Set up infrastructure

```bash
./deploy/setup.sh
```

Creates: global static IP, GKE Autopilot cluster (skipped if `CLUSTER_NAME` already exists), Cloud SQL MySQL instance (db-f1-micro, private IP), database user, admin token, Workload Identity binding, K8s secrets. Credentials saved to `deploy/.secrets.env`. Takes 5-10 minutes.

### 2. Configure DNS

Create an A record at your DNS provider:

```
feeds.example.org.  A  <STATIC_IP>
```

The IP is printed by `setup.sh`. Retrieve it later with:

```bash
gcloud compute addresses describe rss2rm-ip --global --format='value(address)'
```

DNS must resolve before the managed certificate can provision.

### 3. Build and deploy

```bash
./deploy/deploy.sh
```

Builds the Docker image, pushes to Artifact Registry, and applies K8s manifests (deployment with Cloud SQL proxy sidecar, NodePort service, Ingress with ManagedCertificate).

The Google-managed TLS certificate provisions automatically once DNS resolves. This takes 10-20 minutes. Check status:

```bash
kubectl describe managedcertificate rss2rm-cert -n rss2rm
```

Once the certificate status is `Active`, the site is available at `https://feeds.example.org`.

### 4. Create a user

The Cloud SQL instance has no public IP — user management is done through the admin API via `kubectl port-forward`:

```bash
kubectl port-forward svc/rss2rm-admin -n rss2rm 9090:9090
```

Then in another terminal:

```bash
curl -X POST http://localhost:9090/admin/users \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"yourpassword"}'
```

### 5. Tear down

```bash
./deploy/teardown.sh
```

Deletes the K8s namespace, Cloud SQL instance (all data), Artifact Registry repository, static IP, and service account. The GKE cluster is **not** deleted by default. To also delete the cluster:

```bash
./deploy/teardown.sh --delete-cluster
```

Requires typing `yes` to confirm.

## Artifacts

| Resource | Type | Notes |
|----------|------|-------|
| Global static IP | `compute.googleapis.com/Address` | Persists across redeploys |
| GKE cluster | `container.googleapis.com/Cluster` | Created by setup if it doesn't exist; not deleted by default |
| Cloud SQL instance | `sqladmin.googleapis.com/Instance` | MySQL 8.0, db-f1-micro, private IP |
| Google Service Account | `iam.googleapis.com/ServiceAccount` | Cloud SQL client role |
| K8s Deployment | `rss2rm` | App + Cloud SQL proxy sidecar |
| K8s Service | `rss2rm` | NodePort (backend for Ingress) |
| K8s Service | `rss2rm-admin` | ClusterIP (admin API, internal only) |
| K8s Ingress | `rss2rm` | Global HTTP(S) LB with static IP |
| K8s ManagedCertificate | `rss2rm-cert` | Auto-renewing TLS for the domain |
| K8s Secret | `rss2rm-db-secrets` | DB credentials |
| K8s Secret | `rss2rm-admin-secrets` | Admin API token |
| Container image | `REGION-docker.pkg.dev/PROJECT/rss2rm/rss2rm:latest` | Built by deploy.sh, stored in Artifact Registry |

## Files

```
deploy/
├── config.env           # Project/region/domain configuration
├── setup.sh             # Create all GCP infrastructure
├── deploy.sh            # Build image + apply K8s manifests
├── teardown.sh          # Delete all resources
├── .secrets.env         # Auto-generated DB password (gitignored)
└── k8s/
    ├── namespace.yaml      # rss2rm namespace
    ├── deployment.yaml     # App + Cloud SQL proxy sidecar
    ├── service.yaml        # NodePort Service + Ingress + ManagedCertificate
    └── admin-service.yaml  # ClusterIP service for admin API (internal)
```

## Estimated monthly cost

| Resource | Estimate |
|----------|----------|
| GKE Autopilot pod (0.25 vCPU, 512Mi) | ~$5-8 (Standard clusters vary) |
| Cloud SQL db-f1-micro | ~$8-10 |
| Global static IP + load balancer | ~$3-5 |
| **Total** | **~$18-25** |

GKE free tier ($74.40/mo) covers the cluster management fee.

## Admin API

The admin API runs on port 9090 inside the pod, exposed only as a ClusterIP service (not in the Ingress, not accessible from the internet). It is protected by a bearer token generated during setup (saved in `deploy/.secrets.env`).

### Web interface

```bash
kubectl port-forward svc/rss2rm-admin -n rss2rm 9090:9090
```

Open `http://localhost:9090/admin/` in your browser. Enter the admin token from `.secrets.env` to connect. The page provides user management: create, list, verify, and delete users.

### CLI access

The admin token is required in the `Authorization` header:

```bash
# Get the token
source deploy/.secrets.env

# List users
curl -H "Authorization: Bearer $ADMIN_TOKEN" http://localhost:9090/admin/users

# Create a user
curl -X POST http://localhost:9090/admin/users \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"temppassword"}'

# Delete a user (cascade-deletes all their data)
curl -X DELETE -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9090/admin/users/<user-id>

# Manually verify a user
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9090/admin/users/<user-id>/verify
```

### Rotating the admin token

Generate a new token and update the K8s secret:

```bash
NEW_TOKEN=$(openssl rand -base64 32)
kubectl create secret generic rss2rm-admin-secrets \
  --namespace=rss2rm --from-literal=token="$NEW_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl rollout restart deployment/rss2rm -n rss2rm
```

## Registration modes

The deployment defaults to `-registration=closed` (no public registration). Users are created via the admin API.

To change the registration mode, edit `deploy/k8s/deployment.yaml` and update the `-registration` arg:

| Mode | Flag | Behavior |
|------|------|----------|
| Closed | `-registration=closed` | No public registration. Create users via admin API. |
| Allowlist | `-registration=allowlist -registration-allowlist=a@x.com,b@x.com` | Only listed emails can register. |
| Open | `-registration=open` | Anyone can register (not recommended for public deployments). |

After editing, redeploy with `./deploy/deploy.sh`.

## Email verification (optional)

Requires an SMTP provider. GCP blocks port 25 but allows 587 and 465. Recommended providers (all have free tiers for GCP):

- **SendGrid**: 100 emails/day free. [Setup guide](https://cloud.google.com/compute/docs/tutorials/sending-mail/using-sendgrid).
- **Mailgun**: 100 emails/day free. [Setup guide](https://cloud.google.com/compute/docs/tutorials/sending-mail/using-mailgun).
- **Google Workspace SMTP relay**: If you have a Workspace account. Uses port 587.

### Setup steps

1. Sign up with your provider and get SMTP credentials.

2. Create the SMTP secret:

```bash
kubectl create secret generic rss2rm-smtp-secrets \
  --namespace=rss2rm \
  --from-literal=host=smtp.sendgrid.net \
  --from-literal=port=587 \
  --from-literal=user=apikey \
  --from-literal=password=SG.your-api-key \
  --from-literal=from=noreply@feeds.example.org
```

3. Edit `deploy/k8s/deployment.yaml` — uncomment the SMTP args and env vars (clearly marked with "Uncomment when using email verification").

4. Redeploy: `./deploy/deploy.sh`

When enabled, new registrations receive a verification email. Unverified accounts are automatically deleted after 24 hours. Admin-created users are verified by default.

## Serve flag reference

| Flag | Default | Description |
|------|---------|-------------|
| `-port` | 8080 | Public API port |
| `-admin-port` | 9090 | Admin API port |
| `-admin-token` | (none) | Bearer token for admin API (no auth if empty) |
| `-poll` | false | Enable background polling |
| `-poll-interval` | 1800 | Poll interval in seconds |
| `-web-dir` | (none) | Static files directory |
| `-db-driver` | sqlite3 | Database driver (sqlite3 or mysql) |
| `-db-dsn` | rss2rm.db | Database connection string |
| `-destinations` | remarkable | Comma-separated enabled destination types |
| `-registration` | open | Registration mode (open, closed, allowlist) |
| `-registration-allowlist` | (none) | Comma-separated allowed emails |
| `-verify-email` | false | Require email verification |
| `-verify-timeout` | 24h | Delete unverified accounts after this duration |
| `-base-url` | http://localhost:8080 | Public URL for verification links |
| `-smtp-host` | (none) | SMTP server host |
| `-smtp-port` | 587 | SMTP server port |
| `-smtp-user` | (none) | SMTP username |
| `-smtp-password` | (none) | SMTP password |
| `-smtp-from` | (none) | SMTP from address |
