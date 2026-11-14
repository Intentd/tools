// Package cluster is a CLI's boundary to a Kubernetes cluster: it creates and
// deletes local kind clusters, checks that kubectl, helm and the cluster are
// available, installs Helm releases and metrics-server, waits for resources,
// and checks, restarts and reads the logs of Deployments. Subpackage
// bootstrap gives one cluster scoped access to another.
package cluster
