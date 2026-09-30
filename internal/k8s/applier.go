package k8s

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

const ManagedByLabel = "app.kubernetes.io/managed-by"
const ManagedByValue = "antcd"

type ResourceID struct {
	GVR       schema.GroupVersionResource
	Namespace string
	Name      string
}

type Applier struct {
	dynamicClient dynamic.Interface
	mapper        meta.RESTMapper
}

func NewApplier() (*Applier, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("could not load cluster config: %w", err)
		}
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}

	discClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating discovery client: %w", err)
	}

	cachedDiscovery := memory.NewMemCacheClient(discClient)
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscovery)

	return &Applier{
		dynamicClient: dynClient,
		mapper:        restMapper,
	}, nil
}

// ApplyManifest injects the antcd label and applies using Server-Side Apply
func (a *Applier) ApplyManifest(ctx context.Context, yamlData []byte, defaultNamespace string) (*ResourceID, error) {
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}

	_, gvk, err := dec.Decode(yamlData, nil, obj)
	if err != nil {
		return nil, fmt.Errorf("decoding yaml: %w", err)
	}

	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("mapping GVK to resource: %w", err)
	}

	// Inject the managed-by label
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[ManagedByLabel] = ManagedByValue
	obj.SetLabels(labels)

	var dr dynamic.ResourceInterface
	ns := obj.GetNamespace()
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if ns == "" {
			ns = defaultNamespace
			obj.SetNamespace(ns)
		}
		dr = a.dynamicClient.Resource(mapping.Resource).Namespace(ns)
	} else {
		dr = a.dynamicClient.Resource(mapping.Resource)
		ns = ""
	}

	data, err := obj.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshaling json: %w", err)
	}

	force := true
	_, err = dr.Patch(ctx, obj.GetName(), "application/apply-patch+yaml", data, metav1.PatchOptions{
		FieldManager: "antcd",
		Force:        &force,
	})
	if err != nil {
		return nil, fmt.Errorf("applying %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}

	return &ResourceID{
		GVR:       mapping.Resource,
		Namespace: ns,
		Name:      obj.GetName(),
	}, nil
}

// Prune deletes resources in the cluster that have the antcd label but are no longer in Git
func (a *Applier) Prune(ctx context.Context, applied []ResourceID) error {
	appliedMap := make(map[string]bool)
	uniqueGVRs := make(map[schema.GroupVersionResource]bool)

	for _, res := range applied {
		key := fmt.Sprintf("%s/%s/%s", res.GVR.String(), res.Namespace, res.Name)
		appliedMap[key] = true
		uniqueGVRs[res.GVR] = true
	}

	// Query each resource kind we manage
	for gvr := range uniqueGVRs {
		list, err := a.dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", ManagedByLabel, ManagedByValue),
		})
		if err != nil {
			log.Printf("[WARN] Prune: Failed to list %s: %v", gvr.Resource, err)
			continue
		}

		for _, item := range list.Items {
			key := fmt.Sprintf("%s/%s/%s", gvr.String(), item.GetNamespace(), item.GetName())
			if !appliedMap[key] {
				log.Printf("[PRUNE] Resource no longer in Git: deleting %s/%s", gvr.Resource, item.GetName())
				var dr dynamic.ResourceInterface
				if item.GetNamespace() != "" {
					dr = a.dynamicClient.Resource(gvr).Namespace(item.GetNamespace())
				} else {
					dr = a.dynamicClient.Resource(gvr)
				}
				if err := dr.Delete(ctx, item.GetName(), metav1.DeleteOptions{}); err != nil {
					log.Printf("[ERROR] Pruning %s/%s failed: %v", gvr.Resource, item.GetName(), err)
				}
			}
		}
	}
	return nil
}
