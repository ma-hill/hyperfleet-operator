## Pre-merge checks
1. Updates to bundle.Dockerfile are also reflected in bundle.konflux.Dockerfile
2. bundle/ is correctly updated before merging
3. config/manager/kustomization.yaml is not wrongly updated

## CI Installation

Once Konflux is in place, the CI pipeline will automatically handle bundle updates:

1. Konflux builds the operator image and publishes it with a new tag
2. `bundle.konflux.Dockerfile` runs `update_bundle.sh` with the new operator image tag
3. `update_bundle.sh` regenerates the bundle manifests with the updated operator image reference
4. The bundle is automatically built and published

(TODO add more information once the konflux pipelines are in place)

## Develpment Installation

### Prerequisite steps
For local development and installation, set your Quay username to automatically configure image paths:

```bash
# Export some variables to set image paths
export QUAY_USER=<YOUR_QUAY_USERNAME>
export IMG_TAG=<GIT_TAG>
git checkout -b <dev-branch> # Checkout dev branch
make image-dev # Pushes to: quay.io/$QUAY_REPO/hyperfleet-operator:<GIT_TAG>

```


### OLM Installation 
Testing hyperfleet-operator installation with OLM

**Image paths with QUAY_REPO set:**
- Operator: `quay.io/$QUAY_REPO/hyperfleet-operator:$DEV_TAG`
- Bundle: `quay.io/$QUAY_REPO/hyperfleet-operator-bundle:$VERSION`

1. **Update bundle with operator image:** - WARNING restore changes once done testing!
   ```bash
   make bundle-override-img
   # Updates bundle/ manifests with the operator image from step 2
   # Alternative: manually edit config/manager/kustomization.yaml
   # Regenerates bundle.Dockerfile + bundle/ and override config/manager/kustomization.yaml
   ```

2. **Build and push bundle image:**
   ```bash
   make bundle-build
   make bundle-push
   # Pushes to: quay.io/$QUAY_REPO/hyperfleet-operator-bundle:<VERSION>
   ```

3. **Quick testing on a k8s cluster:**
    ```bash
    # Install Operator Lifecycle Manager in your cluster
    operator-sdk olm install
    
    # Install operator from bundle
    operator-sdk run bundle quay.io/$QUAY_REPO/hyperfleet-operator-bundle:<VERSION> -n <NAMESPACE>
    
    # Cleanup when done
    operator-sdk cleanup hyperfleet-operator -n <NAMESPACE>

    # Uninstall Operator Lifecycle Manager from your cluster
    operator-sdk olm uninstall
    ```


### Non-OLM Installation

Testing hyperfleet-operator installation without OLM (kubectl apply)

1. **Create dist/install.yaml with override image:**
    ```bash
    # Generate install.yaml with your custom image
    make build-deployer-override-img

    # Alternative: manually edit config/manager/kustomization.yaml
    # and run `make build-deployer`
    # Generates: dist/install.yaml
    # Again, make sure to restore config/manager/kustomization.yaml after testing
    ```

2. **Quick testing on a k8s cluster:**
    ```bash
    make deploy
    # Check status to see that everything installed properly
    make undeploy
    ```


**Note:** `bundle-override-img` and `build-deployer-override-img` modify config/manager/kustomization.yaml in place. So before committing any chnages make sure to revert these changes. Addiitonally when running `bundle-override-img` the bundle/ and bundle.Dockerfile get regenerated in place, so make sure to check these changes before committing them.



