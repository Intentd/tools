package children

import (
	"context"

	"github.com/intentd/tools/kubeclient"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnrichGroupWithPV embeds the bound PV object under "_pv" for each PVC in
// the group. A no-op when pvc enrichment is not enabled on the CRD.
// The PV name comes from spec.volumeName on the PVC, set by Kubernetes once bound.
func EnrichGroupWithPV(ctx context.Context, kube kubeclient.Interface, m map[string]interface{}) {

	for _, v := range m {
		obj, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		spec, _ := obj["spec"].(map[string]interface{})
		if spec == nil {
			continue
		}
		volName, _ := spec["volumeName"].(string)
		if volName == "" {
			continue
		}
		u, err := kube.DynamicClient().
			Resource(PersistentVolumeGVR).
			Get(ctx, volName, metav1.GetOptions{})
		if err != nil {
			continue
		}
		pvObj := u.Object
		if s, ok := pvObj["status"]; !ok || s == nil {
			pvObj["status"] = map[string]interface{}{}
		}
		obj["_pv"] = pvObj
	}
}
