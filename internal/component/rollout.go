package component

// RolloutTarget identifies the ConfigMap and Deployment that participate in a
// component's config-hash rollout. The controller hashes the ConfigMap's data
// key together with referenced-secret resourceVersions and stamps the result
// onto the Deployment's pod-template annotations so a config or secret change
// rolls the pods.
type RolloutTarget struct {
	ConfigMapName  string
	ConfigFileKey  string
	DeploymentName string
}
