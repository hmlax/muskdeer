package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterCRUDUpdatesBothProjectsIdempotently(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "operator")
	repoProviders := filepath.Join(app, "internal/data/providers.go")
	serviceProviders := filepath.Join(app, "internal/service/providers.go")
	manifestPath := filepath.Join(root, "scripts/admin/tools/gateway-services.json")
	for _, path := range []string{repoProviders, serviceProviders} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{
		repoProviders:    "package data\nvar ProviderSet = wire.NewSet(\n// register:repo\n)\n",
		serviceProviders: "package service\nvar ProviderSet = wire.NewSet(\n// register:service\n)\n",
	}
	manifest := gatewayManifest{
		Parameters:      "// register:param ── 新模块服务形参在此行后注册(make register 工具锚点,勿删)",
		Implementations: map[string]string{},
		Services:        map[string]string{},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for path, source := range sources {
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(manifestPath, encoded, 0644); err != nil {
		t.Fatal(err)
	}
	if err := registerCRUD("product", app); err != nil {
		t.Fatal(err)
	}
	snapshots := map[string]string{}
	for _, path := range []string{repoProviders, serviceProviders, manifestPath} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		snapshots[path] = string(raw)
	}
	if err := registerCRUD("product", app); err != nil {
		t.Fatal(err)
	}
	for path, first := range snapshots {
		second, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if first != string(second) {
			t.Fatalf("re-registering a CRUD module changed %s", path)
		}
	}
	if strings.Count(snapshots[repoProviders], "NewProductRepo,") != 1 || strings.Count(snapshots[serviceProviders], "NewProductService,") != 1 {
		t.Fatal("registration did not add each constructor to its Wire provider set exactly once")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var out gatewayManifest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Services["ProductService"] != "productService" || out.Implementations["productService"] != "ProductService" || strings.Count(out.Parameters, "productService *service.ProductService,") != 1 {
		t.Fatal("registration did not declare the shared admin/operator transport contract")
	}
}
