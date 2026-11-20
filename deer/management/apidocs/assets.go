// Package apidocs embeds the administration OpenAPI and RBAC assets.
package apidocs

import _ "embed"

// OpenAPIData contains the embedded administration API contract.
//
//go:embed openapi.yaml
var OpenAPIData []byte

// OpaRbacRego contains the embedded authorization policy.
//
//go:embed rbac.rego
var OpaRbacRego []byte
