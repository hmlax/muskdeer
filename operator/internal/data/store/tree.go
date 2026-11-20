package store

import (
	"context"
	"fmt"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"
)

// DescendantIDs traverses the generated model using its parent column and the
// same tenant scope as ordinary reads; cycles cannot make the traversal loop.
func DescendantIDs[T any](ctx context.Context, client *DBClient, parentID uint32) ([]uint32, error) {
	meta, err := modelSchema[T]()
	if err != nil {
		return nil, err
	}
	if meta.LookUpField("parent_id") == nil {
		return nil, fmt.Errorf("model has no parent column")
	}
	seen := map[uint32]struct{}{parentID: {}}
	frontier := []uint32{parentID}
	var result []uint32
	entity := &EntityClient[T]{client: client.Client()}
	for len(frontier) > 0 {
		arguments := make([]any, len(frontier))
		for i, id := range frontier {
			arguments[i] = id
		}
		ids, err := entity.Query().Where(queryscope.In("parent_id", arguments...)).IDs(ctx)
		if err != nil {
			return nil, err
		}
		frontier = nil
		for _, id := range ids {
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, id)
			frontier = append(frontier, id)
		}
	}
	return result, nil
}
