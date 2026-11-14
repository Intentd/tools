// Package access gives a hub cluster least-privilege access to target
// clusters, and takes it away. A Config lists the targets with defaults and
// per-cluster settings; Grant creates a ServiceAccount, a ClusterRole with
// the given rules, its binding and a token on a target, and keeps the
// credential on the hub in a HubFormat: a plain Secret (the default), Argo
// CD's cluster Secret, or a kubeconfig (under the key Flux reads, or the
// general one). Revoke removes
// all of it.
//
// Tokens are short by default: a TokenRequest token with a TTL, which the
// ServiceAccount may renew itself, so Refresh, run on the hub, replaces it
// before it expires without an admin. A long token is a token Secret that
// does not expire, reused unless rotated.
package access
