// openapigen applies protobuf response overrides after protoc-gen-openapi.
// Gnostic merges repeated response entries, which otherwise emits duplicate YAML
// status keys when an HTTP file or SSE RPC overrides the default JSON response.
package main

import (
	"fmt"
	"os"

	v3 "github.com/google/gnostic-models/openapiv3"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: openapigen <openapi.yaml>")
	}
	if err := rewrite(os.Args[1]); err != nil {
		panic(err)
	}
}

func rewrite(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc, err := v3.ParseDocument(data)
	if err != nil {
		return fmt.Errorf("parse generated OpenAPI: %w", err)
	}
	for _, path := range doc.GetPaths().GetPath() {
		item := path.GetValue()
		for _, operation := range []*v3.Operation{item.GetGet(), item.GetPost(), item.GetPut(), item.GetPatch(), item.GetDelete(), item.GetHead(), item.GetOptions(), item.GetTrace()} {
			responses := operation.GetResponses()
			if responses == nil {
				continue
			}
			positions := make(map[string]int)
			unique := make([]*v3.NamedResponseOrReference, 0, len(responses.GetResponseOrReference()))
			for _, response := range responses.GetResponseOrReference() {
				if position, ok := positions[response.GetName()]; ok {
					unique[position] = response
					continue
				}
				positions[response.GetName()] = len(unique)
				unique = append(unique, response)
			}
			responses.ResponseOrReference = unique
		}
	}
	data, err = doc.YAMLValue("Generated from protobuf with protoc-gen-openapi and scripts/admin/tools/openapigen. DO NOT EDIT.")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
