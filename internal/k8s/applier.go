package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

type Applier struct {
	dynamicClient dynamic.Interface
	mapper        meta.RESTMapper
}

func NewApplier() (*Applier, error) {
	// Try in-cluster config first, fallback to ~/.kube/config for local dev
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

// ApplyManifest applies raw YAML content to the cluster using Server-Side Apply
func (a *Applier) ApplyManifest(ctx context.Context, yamlData []byte, defaultNamespace string) error {
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}

	_, gvk, err := dec.Decode(yamlData, nil, obj)
	if err != nil {
		return fmt.Errorf("decoding yaml: %w", err)
	}

	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("mapping GVK to resource: %w", err)
	}

	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = defaultNamespace
		}
		dr = a.dynamicClient.Resource(mapping.Resource).Namespace(ns)
	} else {
		dr = a.dynamicClient.Resource(mapping.Resource)
	}

	data, err := obj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshaling json: %w", err)
	}

	force := true
	_, err = dr.Patch(ctx, obj.GetName(), "application/apply-patch+yaml", data, metav1.PatchOptions{
		FieldManager: "antcd",
		Force:        &force,
	})
	if err != nil {
		return fmt.Errorf("applying resource %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}

	return nil
}
