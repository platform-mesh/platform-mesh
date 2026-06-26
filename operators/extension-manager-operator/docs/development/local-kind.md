### Debug Helm chart locally

```sh
# create local KIND cluster
cat <<EOF > kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
EOF
kind create cluster --config ./kind-config.yaml

IMG_TAG=0.16.0

# build docker local chart image
docker build . --no-cache --tag local-extension-manager-operator:$IMG_TAG

# load image to kind
kind load docker-image local-extension-manager-operator:$IMG_TAG

# apply CRDS
kubectl apply -f chart/crds/ui.platform-mesh.io_contentconfigurations.yaml

# change in imagePullPolicy in chart/templates/deployment.yaml
imagePullPolicy: IfNotPresent

# apply chart with test configuration
helm template -f ./chart/test-values.yaml extension-manager-operator --include-crds ./chart/ | kubectl apply -f -

# create sample resources
kubectl apply -f config/samples/ui_v1alpha1_contentconfiguration.yaml

# cleanup
kubectl delete -f config/samples/ui_v1alpha1_contentconfiguration.yaml
helm template -f ./chart/test-values.yaml extension-manager-operator ./chart/ --include-crds | kubectl delete -f -
kubectl delete -f chart/crds/ui.platform-mesh.io_contentconfigurations.yaml
docker image rm local-extension-manager-operator:test
kind delete cluster
```
