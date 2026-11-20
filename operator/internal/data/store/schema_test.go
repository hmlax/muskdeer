package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

type schemaBaseline struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	Columns []struct {
		Name      string `json:"name"`
		Nullable  bool   `json:"nullable"`
		Increment bool   `json:"increment"`
	} `json:"columns"`
	Indexes []struct {
		Name    string   `json:"name"`
		Unique  bool     `json:"unique"`
		Columns []string `json:"columns"`
	} `json:"indexes"`
}

// The snapshot describes the retained framework database, not runtime table selection.
// It prevents a generated model from silently changing a table/column or index.
func TestGeneratedModelsPreserveDatabaseShape(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../deer/proto/operator/testdata/core_schema.json"))
	require.NoError(t, err)
	var baseline []schemaBaseline
	require.NoError(t, json.Unmarshal(data, &baseline))
	models := AllModels()
	require.Len(t, models, len(baseline))
	byModel := make(map[string]any, len(models))
	for _, model := range models {
		byModel[reflect.TypeOf(model).Elem().Name()] = model
	}
	var cache sync.Map
	for _, expected := range baseline {
		t.Run(expected.Name, func(t *testing.T) {
			model := byModel[expected.Model]
			require.NotNil(t, model)
			_, hasManualName := model.(interface{ TableName() string })
			require.False(t, hasManualName)
			metadata, err := schema.Parse(model, &cache, schema.NamingStrategy{SingularTable: true})
			require.NoError(t, err)
			require.Equal(t, expected.Name, metadata.Table)
			require.Len(t, metadata.DBNames, len(expected.Columns))
			for _, column := range expected.Columns {
				field := metadata.LookUpField(column.Name)
				require.NotNil(t, field, "missing column %s", column.Name)
				require.Equal(t, !column.Nullable, field.NotNull, "nullability: %s", column.Name)
				require.Equal(t, column.Increment, field.AutoIncrement, "auto increment: %s", column.Name)
			}
			indexes := metadata.ParseIndexes()
			for _, index := range expected.Indexes {
				var actual *schema.Index
				for _, candidate := range indexes {
					if candidate.Name == index.Name {
						actual = candidate
						break
					}
				}
				require.NotNil(t, actual, "missing index %s", index.Name)
				if index.Unique {
					require.Equal(t, "UNIQUE", actual.Class)
				}
				columns := make([]string, 0, len(actual.Fields))
				for _, field := range actual.Fields {
					columns = append(columns, field.DBName)
				}
				require.Equal(t, index.Columns, columns, "index column order: %s", index.Name)
			}
		})
	}
}
