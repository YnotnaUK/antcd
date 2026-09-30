package k8s

import "sort"

// kindOrder defines the apply sequence matching standard kubectl behavior
var kindOrder = map[string]int{
	"Namespace":                1,
	"CustomResourceDefinition": 2,
	"StorageClass":             3,
	"ServiceAccount":           4,
	"Role":                     5,
	"ClusterRole":              6,
	"RoleBinding":              7,
	"ClusterRoleBinding":       8,
	"ConfigMap":                9,
	"Secret":                   10,
	"PersistentVolume":         11,
	"PersistentVolumeClaim":    12,
	"Service":                  13,
	"Deployment":               14,
	"StatefulSet":              15,
	"DaemonSet":                16,
	"Ingress":                  17,
	"Job":                      18,
	"CronJob":                  19,
}

type ManifestItem struct {
	Kind     string
	Data     []byte
	FileName string
}

// SortManifests orders items so prerequisites (Namespaces, RBAC, Config) apply first
func SortManifests(items []ManifestItem) {
	sort.SliceStable(items, func(i, j int) bool {
		orderI := kindOrder[items[i].Kind]
		if orderI == 0 {
			orderI = 100 // Unlisted kinds apply near the end
		}

		orderJ := kindOrder[items[j].Kind]
		if orderJ == 0 {
			orderJ = 100
		}

		return orderI < orderJ
	})
}
