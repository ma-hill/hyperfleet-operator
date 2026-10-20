package component

// ConfigEntry identifies one ConfigMap data key that feeds the rollout hash.
type ConfigEntry struct {
	ConfigMapName string
	ConfigFileKey string
}

// RolloutTarget identifies the ConfigMaps and Deployment that participate in a
// component's config-hash rollout. The controller hashes every ConfigEntry's
// data key together with referenced-secret resourceVersions and stamps the
// result onto the Deployment's pod-template annotations so a config or secret
// change rolls the pods.
type RolloutTarget struct {
	ConfigEntries  []ConfigEntry
	DeploymentName string
}

// SecretReference pairs a role name (used as the hash entry ID, e.g.
// "database", "broker") with the Kubernetes Secret name the component
// consumes. Components return these from ReferencedSecrets so the controller
// knows which Secrets to include in each component's rollout hash.
type SecretReference struct {
	Role       string
	SecretName string
}
