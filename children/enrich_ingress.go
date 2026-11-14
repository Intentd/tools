package children

import (
	"context"

	"github.com/intentd/tools/kubeclient"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnrichGroupWithIngressData attaches, for each Ingress in the group, the TLS
// Secrets named in spec.tls[*].secretName (native) under _tlsSecrets. A
// no-op when ingress enrichment is not enabled on the CRD. Load-balancer
// addresses are on the Ingress itself (status.loadBalancer.ingress).
func EnrichGroupWithIngressData(ctx context.Context, kube kubeclient.Interface, m map[string]interface{}) {

	for _, v := range m {
		obj, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		meta, _ := obj["metadata"].(map[string]interface{})
		if meta == nil {
			continue
		}
		ns, _ := meta["namespace"].(string)

		// _tlsSecrets from spec.tls[*].secretName
		if ns == "" {
			continue
		}
		spec, _ := obj["spec"].(map[string]interface{})
		if spec == nil {
			continue
		}
		tlsList, _ := spec["tls"].([]interface{})
		var secrets []interface{}
		seen := map[string]bool{}
		for _, tls := range tlsList {
			tlsMap, _ := tls.(map[string]interface{})
			if tlsMap == nil {
				continue
			}
			secretName, _ := tlsMap["secretName"].(string)
			if secretName == "" || seen[secretName] {
				continue
			}
			seen[secretName] = true
			u, err := kube.DynamicClient().
				Resource(SecretGVR).
				Namespace(ns).
				Get(ctx, secretName, metav1.GetOptions{})
			if err != nil {
				continue
			}
			secrets = append(secrets, u.Object)
		}
		if len(secrets) > 0 {
			obj["_tlsSecrets"] = secrets
		}
	}
}
