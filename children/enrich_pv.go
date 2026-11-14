package children

import (
	"context"

	"github.com/intentd/tools/kubeclient"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnrichGroupWithPVC embeds the bound PVC object under "_pvc" for each PV in
// the group. A no-op when pv enrichment is not enabled on the CRD.
// The PVC reference comes from spec.claimRef on the PV, set by Kubernetes once bound.
func EnrichGroupWithPVC(ctx context.Context, kube kubeclient.Interface, m map[string]interface{}) {

	for _, v := range m {
		obj, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		spec, _ := obj["spec"].(map[string]interface{})
		if spec == nil {
			continue
		}
		claimRef, _ := spec["claimRef"].(map[string]interface{})
		if claimRef == nil {
			continue
		}
		ns, _ := claimRef["namespace"].(string)
		name, _ := claimRef["name"].(string)
		if name == "" || ns == "" {
			continue
		}
		u, err := kube.DynamicClient().
			Resource(PersistentVolumeClaimGVR).
			Namespace(ns).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			continue
		}
		obj["_pvc"] = u.Object
	}
}
