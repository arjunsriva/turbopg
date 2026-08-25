package turbopg

import (
	"context"
	"fmt"
)

// CopyNamespace copies all documents from source into dest. Dest is created
// with the source dimensions and index config if it does not already exist.
func (s *Store) CopyNamespace(ctx context.Context, dest, source string) (int64, error) {
	src, err := s.GetNamespace(ctx, source)
	if err != nil {
		return 0, err
	}
	if dest == source {
		return 0, fmt.Errorf("source and destination namespaces must differ")
	}

	dst, err := s.GetNamespace(ctx, dest)
	if IsNotFound(err) {
		opts := CreateNamespaceOptions{Dimensions: src.Dimensions}
		if src.IndexConfig != nil {
			cfg := *src.IndexConfig
			opts.IndexConfig = &cfg
		}
		if err := s.CreateNamespace(ctx, dest, opts); err != nil {
			return 0, err
		}
		if len(src.Schema) > 0 {
			if err := s.UpdateSchema(ctx, dest, src.Schema); err != nil {
				return 0, err
			}
		}
	} else if err != nil {
		return 0, err
	} else if dst.Dimensions != src.Dimensions {
		return 0, fmt.Errorf("destination dimensions %d do not match source %d", dst.Dimensions, src.Dimensions)
	} else {
		dstTable := SQLIdent(GetNamespaceTableName(s.prefix, dest))
		var existing int64
		if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, dstTable)).Scan(&existing); err != nil {
			return 0, fmt.Errorf("count destination: %w", err)
		}
		if existing > 0 {
			return 0, fmt.Errorf("destination namespace must be empty")
		}
		if len(src.Schema) > 0 {
			if err := s.UpdateSchema(ctx, dest, src.Schema); err != nil {
				return 0, err
			}
		}
	}

	srcTable := SQLIdent(GetNamespaceTableName(s.prefix, source))
	dstTable := SQLIdent(GetNamespaceTableName(s.prefix, dest))
	result, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s
		SELECT * FROM %s
		ON CONFLICT (id) DO UPDATE SET
			vector = EXCLUDED.vector,
			attributes = EXCLUDED.attributes%s`, dstTable, srcTable, extraUpdateSQL(extraVectorDests(src.Schema))))
	if err != nil {
		return 0, fmt.Errorf("copy namespace: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("get rows affected: %w", err)
	}
	s.logger.Info("copied namespace",
		Field{Key: "source", Value: source},
		Field{Key: "dest", Value: dest},
		Field{Key: "count", Value: n},
	)
	if err := s.ensureVectorIndexes(ctx, dest); err != nil {
		return n, err
	}
	return n, nil
}
