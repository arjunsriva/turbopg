package turbopg

import (
	"context"
	"fmt"
)

// Delete removes documents by their IDs from a namespace
func (s *Store) Delete(ctx context.Context, namespace string, ids []DocumentID) error {
	return s.DeleteWithCondition(ctx, namespace, ids, nil)
}

// DeleteWithCondition removes listed IDs that also match condition. A nil
// condition deletes every listed ID that exists.
func (s *Store) DeleteWithCondition(ctx context.Context, namespace string, ids []DocumentID, condition Filter) error {
	_, err := s.Write(ctx, Write{
		Namespace:       namespace,
		Deletes:         ids,
		DeleteCondition: condition,
	})
	return err
}

// DeleteByFilter removes documents that match the given filter from a namespace.
func (s *Store) DeleteByFilter(ctx context.Context, namespace string, filter Filter) (int64, error) {
	result, err := s.Write(ctx, Write{
		Namespace:      namespace,
		DeleteByFilter: filter,
	})
	return int64(result.Deleted), err
}

// ClearNamespaceData removes all documents from a namespace without dropping it.
func (s *Store) ClearNamespaceData(ctx context.Context, namespace string) error {
	if err := ValidateNamespace(namespace); err != nil {
		return fmt.Errorf("invalid namespace name: %w", err)
	}
	if _, err := s.GetNamespace(ctx, namespace); err != nil {
		return err
	}

	tableName := SQLIdent(GetNamespaceTableName(s.prefix, namespace))
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("TRUNCATE TABLE %s", tableName)); err != nil {
		return fmt.Errorf("clear namespace: %w", err)
	}

	s.logger.Info("cleared namespace",
		Field{Key: "namespace", Value: namespace},
	)
	return nil
}
