package engine

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Import is a DMN <import> element: a reference to another model whose
// elements are made available under Name (e.g. "myimport.Say Hello(...)").
type Import struct {
	Namespace   string `xml:"namespace,attr"`
	Name        string `xml:"name,attr"`
	LocationURI string `xml:"locationURI,attr"`
}

// ImportResolver returns the raw DMN XML for an import.
type ImportResolver func(imp Import) ([]byte, error)

// ParseWithResolver is Parse, but also loads every <import> (recursively)
// through resolve so the model can be evaluated with its imported decisions,
// business knowledge models and item definitions. Parse alone leaves imports
// unresolved.
func ParseWithResolver(data []byte, resolve ImportResolver) (Definitions, error) {
	return parseWithResolver(data, resolve, map[string]bool{})
}

func parseWithResolver(data []byte, resolve ImportResolver, loading map[string]bool) (Definitions, error) {
	d, err := Parse(data)
	if err != nil {
		return d, err
	}

	if len(d.Imports) == 0 {
		return d, nil
	}

	if loading[d.Namespace] {
		return d, fmt.Errorf("circular import of namespace %q", d.Namespace)
	}
	loading[d.Namespace] = true
	defer delete(loading, d.Namespace)

	d.imported = make(map[string]*Definitions, len(d.Imports))
	for _, imp := range d.Imports {
		raw, err := resolve(imp)
		if err != nil {
			return d, fmt.Errorf("import %q (%s): %w", imp.Name, imp.Namespace, err)
		}

		m, err := parseWithResolver(raw, resolve, loading)
		if err != nil {
			return d, fmt.Errorf("import %q (%s): %w", imp.Name, imp.Namespace, err)
		}

		d.imported[imp.Name] = &m
	}

	return d, nil
}

// ParseFile parses the DMN file at path and resolves its imports from the
// same directory: by locationURI when given, otherwise by finding the
// sibling .dmn file whose namespace matches the import's.
func ParseFile(path string) (Definitions, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Definitions{}, err
	}

	dir := filepath.Dir(path)
	return ParseWithResolver(data, func(imp Import) ([]byte, error) {
		if imp.LocationURI != "" {
			if b, err := os.ReadFile(filepath.Join(dir, imp.LocationURI)); err == nil {
				return b, nil
			}
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".dmn") {
				continue
			}

			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}

			var head struct {
				Namespace string `xml:"namespace,attr"`
			}
			if xml.Unmarshal(b, &head) == nil && head.Namespace == imp.Namespace {
				return b, nil
			}
		}

		return nil, fmt.Errorf("no model found with namespace %q", imp.Namespace)
	})
}

// hrefNamespace returns the namespace part of a fully-qualified href
// ("ns#_id"), or "" for a local one ("#_id").
func hrefNamespace(href string) string {
	if i := strings.LastIndex(href, "#"); i > 0 {
		return href[:i]
	}
	return ""
}

// isForeignHref reports whether href points into another (imported) model.
func isForeignHref(ownNamespace, href string) bool {
	ns := hrefNamespace(href)
	return ns != "" && ns != ownNamespace
}

// importedKey is the key under which an imported decision's output is stored
// alongside the model's own, which can't collide with a local ID.
func importedKey(namespace, id string) string {
	return namespace + "#" + id
}

// evalImports evaluates every imported model against context and returns:
// scope, a context per import name holding that model's decision values and
// callable BKMs (what "Import Name.Member" resolves against); outputs, the
// imported decision values keyed by importedKey; and the imported item
// definitions, keyed "Import Name.typeName" (and bare if not already taken).
func (d Definitions) evalImports(context map[string]any, strict bool) (scope, outputs map[string]any, itemDefs map[string]ItemDefinition, err error) {
	scope = map[string]any{}
	outputs = map[string]any{}
	itemDefs = map[string]ItemDefinition{}

	for _, imp := range d.Imports {
		m, ok := d.imported[imp.Name]
		if !ok {
			continue
		}

		out, err := m.evaluate(context, nil, nil, nil, strict)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("import %q: %w", imp.Name, err)
		}

		members := map[string]any{}
		for _, dec := range m.Decisions {
			if v, ok := out[dec.ID]; ok {
				members[dec.Name] = v
				outputs[importedKey(imp.Namespace, dec.ID)] = v
			}
		}

		mItems := make(map[string]ItemDefinition, len(m.ItemDefinition))
		for _, it := range m.ItemDefinition {
			mItems[it.Name] = it
			itemDefs[imp.Name+"."+it.Name] = it
		}

		bkmMap := make(map[string]BusinessKnowledgeModel, len(m.BusinessKnowledgeModels))
		for _, b := range m.BusinessKnowledgeModels {
			bkmMap[b.ID] = b
		}
		for _, b := range m.BusinessKnowledgeModels {
			members[b.Variable.Name] = bkmFunc(b, bkmMap, mItems)
		}

		scope[imp.Name] = members
	}

	return scope, outputs, itemDefs, nil
}
