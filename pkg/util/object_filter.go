package util

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kcache "k8s.io/client-go/tools/cache"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
)

func MinimalObjectTransform(keepKeys ...string) kcache.TransformFunc {
	keep := make(map[string]struct{}, len(keepKeys))
	for _, k := range keepKeys {
		keep[k] = struct{}{}
	}
	stripMF := crcache.TransformStripManagedFields()

	return func(obj interface{}) (interface{}, error) {
		if trimmed, err := stripMF(obj); err == nil {
			obj = trimmed
		}
		switch o := obj.(type) {
		case *unstructured.Unstructured:
			return stripU(o, keep), nil
		case *unstructured.UnstructuredList:
			for i := range o.Items {
				u := stripU(&o.Items[i], keep)
				o.Items[i] = *u
			}
			return o, nil
		default:
			return obj, nil
		}
	}
}

func stripU(in *unstructured.Unstructured, keep map[string]struct{}) *unstructured.Unstructured {
	out := &unstructured.Unstructured{}
	out.SetAPIVersion(in.GetAPIVersion())
	out.SetKind(in.GetKind())
	out.SetName(in.GetName())
	out.SetNamespace(in.GetNamespace())
	out.SetUID(in.GetUID())
	out.SetResourceVersion(in.GetResourceVersion())
	if ts := in.GetDeletionTimestamp(); ts != nil {
		out.SetDeletionTimestamp(ts)
	}
	if anns := in.GetAnnotations(); len(anns) > 0 {
		filtered := make(map[string]string, 4)
		for k, v := range anns {
			if _, ok := keep[k]; ok {
				filtered[k] = v
			}
		}
		if len(filtered) > 0 {
			out.SetAnnotations(filtered)
		}
	}
	return out
}
