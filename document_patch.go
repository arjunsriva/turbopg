package turbopg

import (
	"context"
)

// Patch merges attributes into existing documents. Documents that do not exist
// are skipped. A non-empty Vector updates the stored embedding (used when the
// server re-embeds an attribute); clients still cannot patch vectors.
func (s *Store) Patch(ctx context.Context, docs []Document, opts UpsertOptions) (int64, error) {
	result, err := s.Write(ctx, Write{
		Namespace:      opts.Namespace,
		Patches:        docs,
		PatchCondition: opts.Condition,
	})
	return int64(result.Patched), err
}

// PatchByFilter merges attributes into every document matching filter.
// A non-empty vector updates the stored embedding for all matching rows.
func (s *Store) PatchByFilter(ctx context.Context, namespace string, filter Filter, attrs map[string]interface{}, vector []float32) (int64, error) {
	result, err := s.Write(ctx, Write{
		Namespace: namespace,
		PatchByFilter: &PatchByFilterWrite{
			Filter:     filter,
			Attributes: attrs,
			Vector:     vector,
		},
	})
	return int64(result.Patched), err
}
