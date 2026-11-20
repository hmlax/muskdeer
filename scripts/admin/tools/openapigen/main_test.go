package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResponseOverrideProducesValidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	data := []byte(`openapi: 3.0.3
info: {title: test, version: "1"}
paths:
  /file:
    get:
      responses:
        "200":
          description: Default JSON
          content:
            application/json:
              schema: {type: object}
        "200":
          description: File override
          content:
            application/octet-stream:
              schema: {type: string, format: binary}
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := rewrite(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Paths map[string]struct {
			Get struct {
				Responses map[string]struct {
					Description string `yaml:"description"`
				} `yaml:"responses"`
			} `yaml:"get"`
		} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if response := result.Paths["/file"].Get.Responses["200"]; response.Description != "File override" {
		t.Fatalf("IDL response override lost: %s", data)
	}
}
