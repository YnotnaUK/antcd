package manifests

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ynotnauk/antcd/internal/k8s"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	serializerYaml "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

// Collect walks dir and returns every Kubernetes document found in .yaml/.yml files.
func Collect(dir string) ([]k8s.ManifestItem, error) {
	var items []k8s.ManifestItem

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		docs, err := ParseDocuments(data, filepath.Base(path))
		if err != nil {
			return err
		}
		items = append(items, docs...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading manifests: %w", err)
	}

	return items, nil
}

// ParseDocuments splits a multi-document YAML stream into manifest items.
// Empty documents are skipped; source is recorded as the item's FileName.
func ParseDocuments(data []byte, source string) ([]k8s.ManifestItem, error) {
	var items []k8s.ManifestItem
	dec := serializerYaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	for {
		doc, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(bytes.TrimSpace(doc)) == 0 {
			continue
		}

		obj := &unstructured.Unstructured{}
		_, gvk, err := dec.Decode(doc, nil, obj)
		if err != nil {
			return nil, fmt.Errorf("decoding kind from %s: %w", source, err)
		}

		items = append(items, k8s.ManifestItem{
			Kind:     gvk.Kind,
			Data:     doc,
			FileName: source,
		})
	}
	return items, nil
}
