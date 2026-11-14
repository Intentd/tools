package children

import (
	"context"

	"github.com/intentd/tools/kubeclient"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnrichGroupWithNode fetches the Node for each Pod and embeds a summary under
// "_node". A no-op when node enrichment is not enabled on the CRD.
//
// Summary: {name, zone, region, instanceType}
// Zone and region come from well-known topology labels.
func EnrichGroupWithNode(ctx context.Context, kube kubeclient.Interface, m map[string]interface{}) {

	nodeCache := map[string]interface{}{}
	for _, v := range m {
		obj, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		spec, _ := obj["spec"].(map[string]interface{})
		if spec == nil {
			continue
		}
		nodeName, _ := spec["nodeName"].(string)
		if nodeName == "" {
			continue
		}
		// Cache to avoid fetching the same node multiple times when
		// a blueprint declares multiple pods on the same node.
		if cached, ok := nodeCache[nodeName]; ok {
			obj["_node"] = cached
			continue
		}
		u, err := kube.DynamicClient().
			Resource(NodeGVR).
			Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			continue
		}
		trimmed := trimNode(u.Object)
		nodeCache[nodeName] = trimmed
		obj["_node"] = trimmed
	}
}
