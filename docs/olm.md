# Hyperfleet-Operator Installation via OLM

## Pre-merge checks
1. Updates to bundle.Dockerfile are also reflected in bundle.konflux.Dockerfile
2. bundle/ is correctly updated before merging
3. config/manager/kustomization.yaml is not wrongly updated

## CI Image Build - Operator Image + Operator Bundle + Operator Catalog

Konflux Workflow:

1. Konflux builds the operator image and publishes it to quay.io/redhat-services-prod/hyperfleet-tenant/hyperfleet/hyperfleet-operator
2. Konflux automatically updates image references for the operator image in the bundle.konflux.Dockerfile. This will automatically get merged.
3. The operator-bundle-push .tekton pipeline will be triggered by the update to the bundle.konflux.Dockerfile. So this new operator image update will trigger the build once merged into main. Note - `bundle.konflux.Dockerfile` runs `update_bundle.sh` with the new operator image reference. `update_bundle.sh` uses yq to update the CSV to ensure the operator deployment has proper values - image, relatedImages, annotations, etc. Once completed the pipeline publishes the operator-bundle to `quay.io/redhat-services-prod/hyperfleet-tenant/hyperfleet/hyperfleet-operator-bundle`
5. Konflux will subsequently update the image reference for the operator-bundle image in `konflux-template.yaml`. Auto-merging the update.
6. The operator-catalog-push .tekton pipeline will be triggered by the update to the `konflux-template.yaml`. This pipeline will build operator-catalog image and push it to `quay.io/redhat-services-prod/hyperfleet-tenant/hyperfleet/hyperfleet-operator-catalog.

> **Note**: Any update to [bundle.konflux.Dockerfile](../bundle.konflux.Dockerfile) will trigger the operator-bundle-push pipeline and any update to [konflux-template.yaml](../catalog/konflux-template.yaml) will trigger the operator-catalog-push pipeline.

> **TODO** - HYPERFLEET-1617 - Update documentation based on release details for the catalog. Currently no upgrade graph for the hyperfleet-operator.

## Developer Installation

### Prerequisites
- go version v1.26.0+
- docker version 18.06+
- operator-sdk v1.42.3+
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.27.0+ cluster.

### Prerequisite steps

For local development and installation, set your Quay username to automatically configure image paths:

1. **Export your quay username:** (Required)
   ```bash
   export QUAY_USER=<YOUR_QUAY_USERNAME>
   ```
2. **Build and push the operator image:**
   ```bash
   make image-dev OPERATOR_IMG=...
   # default OPERATOR_IMG=quay.io/$QUAY_USER/hyperfleet-operator:dev-<git-sha>
   ```
3. **Update the bundle with your OPERATOR_IMG:**
   ```bash
   make bundle-override-img OPERATOR_IMG=...
   ```
**Note** When running `bundle-override-img` the bundle/ and bundle.Dockerfile get regenerated in place, so make sure to check these changes before committing them.
4. **Build the bundle image:**
   ```bash
   make bundle-build BUNDLE_IMG=...
   # default BUNDLE_IMG=quay.io/$QUAY_USER/hyperfleet-operator-bundle:v$(VERSION)
   # default VERSION = 0.0.1
   ```
5. **Push the bundle image:**
   ```bash
   make bundle-push BUNDLE_IMG=...
   ```

**Image path defaults with QUAY_USER set:**
- OPERATOR_IMG (hyperfleet-operator): `quay.io/$QUAY_USER/hyperfleet-operator:dev-<git-sha>`
- BUNDLE_IMG (hyperfleet-operator-bundle): `quay.io/$QUAY_USER/hyperfleet-operator-bundle:v$(VERSION)` (default VERSION=0.0.1)
- CATALOG_IMG (hyperfleet-operator-catalog): `quay.io/$QUAY_USER/hyperfleet-operator-catalog:v$(VERSION)` (defaul VERSION=0.0.1)


### Bundle + Catalog with OLM Classic V0
Testing hyperfleet-operator installation with OLM using a catalog image.

The catalog build uses a template system with a base template (`catalog/base-template.yaml`) that defines the package and channel, and environment-specific templates that specify the bundle image:
- `catalog/dev-template.yaml` - for local development (default)
- `catalog/konflux-template.yaml` - for Konflux CI builds

**Note:** Ensure `QUAY_USER`, `BUNDLE_IMG` and `CATALOG_IMG` are set before running these commands
**Note:** Make sure you have completed the steps in [Prerequisite Steps](#prerequisite-steps)

1. **Update the catalog template with the new bundle image:**
   ```bash
   make catalog-template-update-bundle-img BUNDLE_IMG=... TEMPLATEFILE=...
   # default TEMPLATEFILE=dev-template.yaml
   # default BUNDLE_IMG=quay.io/$QUAY_USER/hyperfleet-operator-bundle:v$(VERSION)
   # updates catalog/<TEMPLATEFILE> with the current BUNDLE_IMG
   ```

2. **Build the catalog image:**
   ```bash
   make catalog-build CATALOG_IMG=...
   # default CATALOG_IMG=quay.io/$QUAY_USER/hyperfleet-operator-catalog:v$(VERSION)
   # default VERSION = 0.0.1
   ```
3. **Push the catalog image:**
   ```bash
   make catalog-push CATALOG_IMG=...
   ```

4. **Deploy on a k8s cluster with OLM:**
   ```bash
   # Install OLM if not already installed
   operator-sdk olm install

   # Create a CatalogSource
   kubectl apply -f - <<EOF
   apiVersion: operators.coreos.com/v1alpha1
   kind: CatalogSource
   metadata:
     name: hyperfleet-operator-catalog
     namespace: <NAMESPACE>
   spec:
     sourceType: grpc
     image: quay.io/$QUAY_USER/hyperfleet-operator-catalog:v$(VERSION)
     displayName: HyperFleet Operator
     publisher: Red Hat
     updateStrategy:
       registryPoll:
         interval: 5m
   EOF

   # Create an OperatorGroup (AllNamespaces mode)
   kubectl apply -f - <<EOF
   apiVersion: operators.coreos.com/v1
   kind: OperatorGroup
   metadata:
     name: hyperfleet-operator-group
     namespace: <NAMESPACE>
   spec: {}
   EOF

   # Create a Subscription
   kubectl apply -f - <<EOF
   apiVersion: operators.coreos.com/v1alpha1
   kind: Subscription
   metadata:
     name: hyperfleet-operator
     namespace: <NAMESPACE>
   spec:
     channel: stable
     name: hyperfleet-operator
     source: hyperfleet-operator-catalog
     sourceNamespace: <NAMESPACE>
     installPlanApproval: Automatic
   EOF

   # Watch the installation
   kubectl get sub,installplan,csv -n <NAMESPACE> -w
   ```

5. **Cleanup:**
    ```bash
    # IMPORTANT: Delete CRs before uninstalling operator
    kubectl get hyperfleetconfig -o yaml > hyperfleetconfig-backup.yaml
    kubectl delete hyperfleetconfig --all

    kubectl delete sub hyperfleet-operator -n <NAMESPACE>
    kubectl delete csv hyperfleet-operator.v0.0.1 -n <NAMESPACE>
    kubectl delete catalogsource hyperfleet-operator-catalog -n <NAMESPACE>
    kubectl delete operatorgroup hyperfleet-operator-group -n <NAMESPACE>

    # Uninstall OLM from your cluster
    operator-sdk olm uninstall
    ```

> **TODO**: HYPERFLEET-1616: OLM V1 install

### Install with operator-sdk and bundle - (operator-sdk run bundle)
Quick testing with `operator-sdk run bundle` (no catalog needed).

**Note:** Ensure `BUNDLE_IMG` is exported before running these commands

3. **Quick testing on a k8s cluster:**
    ```bash
    # Install Operator Lifecycle Manager in your cluster
    operator-sdk olm install
    
    # Install hyperfleet-operator from bundle
    operator-sdk run bundle $BUNDLE_IMG -n <NAMESPACE>
   
    # Apply an example hyperfleetconfig cr
    kubectl apply -k config/samples/
    
    # Cleanup when done - IMPORTANT: Delete CRs before uninstalling operator
    kubectl delete hyperfleetconfig --all
    
    # Uninstall hyperfleet-operator
    operator-sdk cleanup hyperfleet-operator -n <NAMESPACE>
    # Uninstall olm
    operator-sdk olm uninstall
    ```
