# Create ClusterAccess script

This script is used to create a ClusterAccess resource, which is needed for kubernetes-graphql-gateway to work with Standard K8S cluster.

More details about it you can find at [this readme](../docs/clusteraccess.md)

## Usage

```shell
./hack/create-clusteraccess.sh --target-kubeconfig $TARGET_CLUSTER_KUBECONFIG --management-kubeconfig $MANAGEMENT_CLUSTER_KUBECONFIG
```
Where
- TARGET_CLUSTER_KUBECONFIG - path to the kubeconfig of the cluster we want the gateway to generate graphql schema
- MANAGEMENT_CLUSTER_KUBECONFIG - path to the kubeconfig of the cluster where ClusterAccess object will be created. It can be the same cluster as TARGET_CLUSTER_KUBECONFIG.
