package children

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/intentd/tools/kubeclient"
	"github.com/intentd/tools/logger"
)

// Ref names one child to read. A namespaced child with no Namespace is read
// in its owner's namespace.
type Ref struct {
	Name      string
	Namespace string
}

// ReadGroup reads each ref of gvr around owner and returns them by name. A
// child that isn't there yet is left out; its status is always a map, so
// templates reading it never meet a nil.
func ReadGroup(
	ctx context.Context,
	kube kubeclient.Interface,
	obj kubeclient.Object,
	gvr schema.GroupVersionResource,
	refs []Ref,
) map[string]interface{} {
	result := map[string]interface{}{}

	for _, child := range refs {
		ns := child.Namespace
		if ns == "" {
			ns = obj.GetNamespace()
		}

		var err error
		var u *unstructured.Unstructured
		if ns != "" {
			// Namespaced — use resolved namespace (template or owner fallback)
			u, err = kube.DynamicClient().
				Resource(gvr).
				Namespace(ns).
				Get(ctx, child.Name, metav1.GetOptions{})
		} else {
			// Cluster-scoped (e.g. Namespace, ClusterRole)
			u, err = kube.DynamicClient().
				Resource(gvr).
				Get(ctx, child.Name, metav1.GetOptions{})
		}

		if err != nil {
			// Not found on first reconcile is expected and not an error.
			// Any other error is worth logging at debug level.
			if !strings.Contains(err.Error(), "not found") {
				logger.FromContext(ctx).Debug().
					Str("resource", fmt.Sprintf("%s/%s", ns, child.Name)).
					Str("gvr", gvr.String()).
					Err(err).
					Msg("children: read failed — omitted from status context")
			}
			continue
		}

		// Ensure status is always a non-nil map so template expressions like
		// {{ .children.deployment.status.readyReplicas }} resolve to "" rather
		// than causing a nil-pointer dereference on a newly-created resource.
		o := u.Object
		if s, ok := o["status"]; !ok || s == nil {
			o["status"] = map[string]interface{}{}
		}
		result[child.Name] = o
	}

	return result
}
