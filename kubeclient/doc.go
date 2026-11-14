// Package kubeclient wraps every Kubernetes client concern: REST config,
// typed and dynamic clients, the GenericClient the informer factory uses to
// build ListerWatchers, and typed CRUD and server-side apply helpers. Build
// one with New (Options says where it connects) or NewFromConfig; the fake
// for tests is in package kubefake.
package kubeclient
