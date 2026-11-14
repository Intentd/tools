package children

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/intentd/tools/builtins"
)

// The built-in resources the enrichers read.
var (
	EndpointSliceGVR         = gvr("endpointslice")
	EventGVR                 = gvr("event")
	JobGVR                   = gvr("job")
	NodeGVR                  = gvr("node")
	PersistentVolumeClaimGVR = gvr("persistentvolumeclaim")
	PersistentVolumeGVR      = gvr("persistentvolume")
	PodGVR                   = gvr("pod")
	ReplicaSetGVR            = gvr("replicaset")
	SecretGVR                = gvr("secret")
	StorageClassGVR          = gvr("storageclass")
)

// gvr is kind's resource from the builtins table; a kind missing from the
// table is a programming error.
func gvr(kind string) schema.GroupVersionResource {
	d, ok := builtins.Lookup(kind)
	if !ok {
		panic(fmt.Sprintf("children: %q is not in the builtins table", kind))
	}
	return schema.GroupVersionResource{Group: d.Group, Version: d.Version, Resource: d.Plural}
}
