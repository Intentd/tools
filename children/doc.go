// Package children reads a custom resource's children and the live state
// around them. ReadGroup reads named children of one resource type; the
// enrichers add to each child what it depends on: the pods behind a
// Deployment, StatefulSet or ReplicaSet, a Service's backing pods and
// endpoints, PVC, PV and StorageClass, an Ingress's state, a Node, a
// CronJob's Jobs, and warning events. Which children to read and which
// enrichments run is the caller's choice.
package children
