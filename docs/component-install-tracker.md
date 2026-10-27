---
Status: Active
Owner: Mallory Hill
Last Updated: 2026-10-26
---
# Component Install Tracker

This document covers the latest requirements and dependencies for the hyperfleet components managed through the operator.

<u>Current components:</u> api, 5 sentinels (clusters, wifconfigs, nodepools, versions, channels)

## Contents

- [Required objects](#required-objects)
- [Component details](#component-details)
  - [API](#api)
  - [Sentinel](#sentinel)
    - [Secrets](#secrets)
- [Local Testing - Kind](#local-testing---kind)
  - [1. Create the cluster and namespace](#1-create-the-cluster-and-namespace)
  - [2. Install the dependencies](#2-install-the-dependencies)
  - [3. Create the Secrets](#3-create-the-secrets)
  - [4. Build and deploy the operator](#4-build-and-deploy-the-operator)
  - [5. Create a HyperFleetConfig](#5-create-a-hyperfleetconfig)
    - [Option A: auth disabled](#option-a-auth-disabled)
    - [Option B: auth enabled](#option-b-auth-enabled)
  - [6. Verify](#6-verify)
  - [7. Cleanup](#7-cleanup)
## Required objects

**Note**: The operator never creates the Secrets below; you must provide them. Every Secret
must live in the **operator's namespace** (`hyperfleet-system` by default).

| Object   | Name                                                                      | Required when                                                    | Contents                                                              |
| -------- | ------------------------------------------------------------------------- | ---------------------------------------------------------------- | --------------------------------------------------------------------- |
| `Secret` | `hyperfleet-db` (any name; must equal `spec.api.database.secretRef.name`) | Always (API)                                                     | `db.host`, `db.port`, `db.name`, `db.user`, `db.password`             |
| `Secret` | `hyperfleet-broker-credentials` (fixed name)                              | Broker type is RabbitMQ, the default (Sentinel)                  | `BROKER_RABBITMQ_URL`                                                 |
| `Secret` | Any name (must equal spec.api.auth.jwkCertSecretRef.name)                 | spec.api.auth.jwkCertSecretRef.name is set in `HyperFleetConfig` | The Secret must provide the key "jwks.json" containing a JSON Web Key |

A missing Secret does not stop the reconcile. The operator hashes it as absent
and sets `Degraded=True` with reason `ReferencedSecretMissing`, listing the
missing Secrets. Creating it afterwards recovers the CR and rolls the affected
pods.

## Component details

### API

- Requires a valid postgreSQL database
- Requires a secret with database credentials

```yaml
# hyperfleet-db.yaml
apiVersion: v1
kind: Secret
type: Opaque
metadata:
  name: hyperfleet-db
  namespace: <operator_namespace>
stringData:
  db.host: <...>
  db.port: <...>
  db.name: <...>
  db.user: <...>
  db.password: <...>
```

### Sentinel

The broker backend is chosen by environment variables on the operator
Deployment, not by the CR:

| Variable                         | Value                        | Notes                                                                            |
| -------------------------------- | ---------------------------- | -------------------------------------------------------------------------------- |
| `BROKER_TYPE`                    | `rabbitmq` or `googlepubsub` | Defaults to `rabbitmq` when unset; any other value stops the operator at startup |
| `BROKER_GOOGLEPUBSUB_PROJECT_ID` | A valid GCP project ID       | Required when `BROKER_TYPE=googlepubsub`                                         |

`make deploy` chooses the broker with the `BROKER_TYPE` make variable (default
`rabbitmq`; any other value except `googlepubsub` fails the build). With
RabbitMQ, no broker variables are added to the operator Deployment, because the
operator already defaults to it. With `googlepubsub`, `make deploy` renders
`config/overlays/googlepubsub`, which adds both variables, and fails if
`BROKER_GOOGLEPUBSUB_PROJECT_ID` is empty:

```bash
make deploy RELATED_IMAGE_HYPERFLEET_OPERATOR=$OPERATOR_IMG \
  BROKER_TYPE=googlepubsub BROKER_GOOGLEPUBSUB_PROJECT_ID=<gcp-project-id>
```

#### Secrets

- `BROKER_TYPE=googlepubsub` does not require any additional secrets/configuration, but does require topic creation. 
- `BROKER_TYPE=rabbitmq` requires a secret named `hyperfleet-broker-credentials` that contains the RabbitMQ url

```yaml
# hyperfleet-broker-credentials.yaml
apiVersion: v1
kind: Secret
type: Opaque
metadata:
  name: hyperfleet-broker-credentials
  namespace: <operator_namespace>
stringData:
  # amqp://user:password@host:port/vhost
  BROKER_RABBITMQ_URL: <...>
```

## Local Testing - Kind

**Note: BROKER_TYPE defaults to rabbitmq**

### 1. Create the cluster and namespace

```bash
kind create cluster --name hyperfleet-kind
export NAMESPACE=hyperfleet-system
kubectl create namespace $NAMESPACE
```

### 2. Install the dependencies

PostgreSQL and RabbitMQ run in the same namespace:

```bash
helm install postgresql <postgres_chart> -n $NAMESPACE
helm install rabbitmq <rabbitmq_chart> -n $NAMESPACE
```

### 3. Create the Secrets

```bash
# Database credentials (required for the API)
kubectl create secret generic hyperfleet-db -n $NAMESPACE \
  --from-literal=db.host=<postgres-service-name> \
  --from-literal=db.port=5432 \
  --from-literal=db.name=<database> \
  --from-literal=db.user=<user> \
  --from-literal=db.password=<password>

# Broker credentials (required for Sentinel with RabbitMQ)
kubectl create secret generic hyperfleet-broker-credentials -n $NAMESPACE \
  --from-literal=BROKER_RABBITMQ_URL=<amqp://user:password@host:port/vhost>
```

The JWKS Secret is only needed for [option B](#option-b-auth-enabled) below.

### 4. Build and deploy the operator

```bash
export OPERATOR_IMG=quay.io/<quay-user>/hyperfleet-operator:dev-<git-sha>
make image-dev QUAY_USER=<quay-user>
make deploy RELATED_IMAGE_HYPERFLEET_OPERATOR=$OPERATOR_IMG
```

`make image-dev` tags the image `dev-<git-sha>`. `make deploy` installs the
CRD and the operator into `hyperfleet-system`.

### 5. Create a HyperFleetConfig

#### Option A: auth disabled

```bash
kubectl apply -f - <<'EOF'
apiVersion: hyperfleet.redhat.com/v1alpha1
kind: HyperFleetConfig
metadata:
  name: cluster
spec:
  bundle: cloud-capi
  api:
    database:
      secretRef:
        name: hyperfleet-db
    auth:
      enabled: false
EOF
```

#### Option B: auth enabled

Kind publishes its service-account signing keys, so use them as the JWKS:

```bash
# Fetch the signing keys and store them as a Secret
kubectl get --raw /openid/v1/jwks > jwks.json
kubectl create secret generic kind-jwks -n $NAMESPACE \
  --from-file=jwks.json=jwks.json

# Confirm the issuer matches the CR below
kubectl get --raw /.well-known/openid-configuration | jq -r .issuer

kubectl apply -f - <<'EOF'
apiVersion: hyperfleet.redhat.com/v1alpha1
kind: HyperFleetConfig
metadata:
  name: cluster
spec:
  bundle: cloud-capi
  api:
    database:
      secretRef:
        name: hyperfleet-db
    auth:
      enabled: true
      issuer: https://kubernetes.default.svc.cluster.local
      audience: hyperfleet-api
      jwkCertSecretRef:
        name: kind-jwks
    profile: small
EOF
```

### 6. Verify

```bash
kubectl get hyperfleetconfigs.hyperfleet.redhat.com cluster \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}): {.message}{"\n"}{end}'
```

A healthy install reports:

```
Degraded=False (AsExpected): no failure detected
Available=True (DeploymentAvailable): all components are available
Progressing=False (RolloutComplete): all components have completed rollout
```

### 7. Cleanup

```bash
# uninstall rabbitmq and postgresql
helm uninstall postgresql -n $NAMESPACE
helm uninstall rabbitmq -n $NAMESPACE

# uninstall the secrets
kubectl delete secret hyperfleet-db -n $NAMESPACE
kubectl delete secret hyperfleet-broker-credentials -n $NAMESPACE

# uninstall the HyperFleetConfig
kubectl delete hyperfleetconfigs.hyperfleet.redhat.com cluster

make undeploy
# delete the cluster
kind delete cluster --name hyperfleet-kind
```
