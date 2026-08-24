package turbopg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// QueryOptions represents options for querying documents
type QueryOptions struct {
	// Namespace to query
	Namespace string

	// Optional: Vector to search by similarity
	// Note: When combining vector search with filters, documents with zero
	// similarity to the query vector may be excluded from results, even if
	// they match the filter conditions. This is an optimization by the
	// underlying pgvector implementation.
	Vector []float32

	// Optional: Filter to apply
	Filter Filter

	// Number of results to return
	TopK int

	// Optional: Distance metric to use for vector search
	// Default: "cosine"
	Metric string

	// Optional: TurboPuffer-style ranking. When empty, Vector is used if set.
	Rank RankSpec

	// Exact disables the IVF index (kNN / recall ground truth).
	Exact bool

	// ComputeAttributes fills QueryResult.Computed with per-clause scores
	// when rank_by is Sum/Product/Max.
	ComputeAttributes bool
}

// QueryResult represents a single search result with its score
type QueryResult struct {
	// Document that matched the query
	Document Document

	// Score represents the similarity/distance score
	// Lower is better for distance metrics (L2 / ANN)
	// Higher is better for BM25 relevance
	Score float64

	// HasScore is false when ranking by an attribute (TurboPuffer omits $dist).
	HasScore bool

	// Computed holds per-clause scores when QueryOptions.ComputeAttributes is set.
	Computed map[string]float64
}

// SearchVector finds the top-K most similar vectors in a namespace
func (s *Store) SearchVector(ctx context.Context, namespace string, vector []float32, topK int, metric string) ([]QueryResult, error) {
	return s.Query(ctx, QueryOptions{
		Namespace: namespace,
		Vector:    vector,
		TopK:      topK,
		Metric:    metric,
	})
}

// SearchFiltered finds the top-K most similar vectors that match the given filter
func (s *Store) SearchFiltered(ctx context.Context, namespace string, vector []float32, filter FilterCondition, topK int, metric string) ([]QueryResult, error) {
	return s.Query(ctx, QueryOptions{
		Namespace: namespace,
		Vector:    vector,
		Filter:    filter,
		TopK:      topK,
		Metric:    metric,
	})
}

// Query searches for documents based on the provided options
func (s *Store) Query(ctx context.Context, opts QueryOptions) ([]QueryResult, error) {
	ns, err := s.GetNamespace(ctx, opts.Namespace)
	if err != nil {
		return nil, err
	}

	spec := opts.Rank
	if spec.empty() && len(opts.Vector) > 0 {
		spec = RankSpec{Kind: RankVector, Vector: opts.Vector}
	}

	if spec.Kind != RankSparse && spec.Kind != RankLate && (spec.Kind == RankVector || len(spec.Vector) > 0) {
		vec := spec.Vector
		if len(vec) == 0 {
			vec = opts.Vector
		}
		wantDims := ns.Dimensions
		if spec.Field != "" && spec.Field != "vector" {
			if extra := embedDimsForField(ns.Schema, spec.Field); extra > 0 {
				wantDims = extra
			}
		} else if ns.Dimensions <= 0 {
			return nil, InvalidInput("vector search requires a vector namespace")
		}
		if wantDims > 0 && len(vec) != wantDims {
			return nil, InvalidInputf("vector dimensions mismatch: got %d, want %d", len(vec), wantDims)
		}
		spec.Vector = vec
		spec.Kind = RankVector
	}

	exact := opts.Exact || spec.Exact

	if opts.TopK <= 0 {
		return nil, InvalidInputf("topK must be positive, got %d", opts.TopK)
	}
	if opts.TopK > MaxTopK {
		return nil, InvalidInputf("top_k exceeds %d", MaxTopK)
	}

	metric := opts.Metric
	if metric == "" && ns.IndexConfig != nil {
		metric = ns.IndexConfig.DistanceMetric
	}
	if metric == "" {
		metric = "cosine_distance"
	}

	if spec.inProcess() {
		results, err := s.queryScoredInProcess(ctx, opts.Namespace, spec, opts.Filter, opts.TopK, metric, exact, ns.Schema)
		if err != nil {
			return nil, err
		}
		if opts.ComputeAttributes {
			attachComputed(results, spec, metric)
		}
		return results, nil
	}

	tableName := SQLIdent(GetNamespaceTableName(s.prefix, opts.Namespace))
	var args []interface{}

	compiled, err := s.compileRank(ctx, opts.Namespace, spec, metric, &args, ns.Schema)
	if err != nil {
		return nil, err
	}

	extraSQL, extraCols := extraVectorSelectSQL(ns.Schema)
	var queryBuilder strings.Builder
	queryBuilder.WriteString(fmt.Sprintf(`
		SELECT id, vector, attributes, %s%s
		FROM %s`, compiled.selectExpr, extraSQL, tableName))

	var whereParts []string
	if compiled.extraWhere != "" {
		whereParts = append(whereParts, compiled.extraWhere)
	}
	if opts.Filter != nil {
		whereClause, filterArgs, err := buildFilterSQL(opts.Filter)
		if err != nil {
			return nil, fmt.Errorf("build filter: %w", err)
		}
		if whereClause != "" {
			whereClause = shiftPlaceholders(whereClause, len(args))
			whereParts = append(whereParts, "("+whereClause+")")
			args = append(args, filterArgs...)
		}
	}
	if len(whereParts) > 0 {
		queryBuilder.WriteString("\nWHERE " + strings.Join(whereParts, " AND "))
	}
	if compiled.orderExpr != "" {
		queryBuilder.WriteString("\nORDER BY " + compiled.orderExpr)
	}
	queryBuilder.WriteString(fmt.Sprintf("\nLIMIT $%d", len(args)+1))
	args = append(args, opts.TopK)

	s.logger.Debug("executing query",
		Field{Key: "namespace", Value: opts.Namespace},
		Field{Key: "arg_count", Value: len(args)},
	)

	results, err := s.executeQueryAndParse(ctx, queryBuilder.String(), args, scanQueryRowWithExtras(extraCols), exact)
	if err != nil {
		return nil, err
	}
	if opts.ComputeAttributes {
		attachComputed(results, spec, metric)
	}

	s.logger.Info("query completed",
		Field{Key: "namespace", Value: opts.Namespace},
		Field{Key: "top_k", Value: opts.TopK},
		Field{Key: "rank", Value: string(spec.Kind)},
		Field{Key: "has_filter", Value: opts.Filter != nil},
		Field{Key: "results", Value: len(results)},
	)

	return results, nil
}

func scanQueryRowWithExtras(extraCols []extraVecCol) func(*sql.Rows) (*QueryResult, error) {
	return func(rows *sql.Rows) (*QueryResult, error) {
		return scanQueryRowExtra(rows, extraCols)
	}
}

func scanQueryRowExtra(rows *sql.Rows, extraCols []extraVecCol) (*QueryResult, error) {
	var (
		doc       Document
		vectorNS  sql.NullString
		attrsJSON []byte
		distance  sql.NullFloat64
	)
	extraNS := make([]sql.NullString, len(extraCols))
	dest := []interface{}{&doc.ID, &vectorNS, &attrsJSON, &distance}
	for i := range extraNS {
		dest = append(dest, &extraNS[i])
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, fmt.Errorf("scan result: %w", err)
	}
	if vectorNS.Valid {
		vec, err := StringToVector(vectorNS.String)
		if err != nil {
			return nil, err
		}
		doc.Vector = vec
	}
	if len(attrsJSON) > 0 {
		if err := json.Unmarshal(attrsJSON, &doc.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal attributes: %w", err)
		}
	} else {
		doc.Attributes = map[string]interface{}{}
	}
	if len(extraCols) > 0 {
		doc.ExtraVectors = map[string][]float32{}
		for i, col := range extraCols {
			if !extraNS[i].Valid {
				continue
			}
			vec, err := StringToVector(extraNS[i].String)
			if err != nil {
				return nil, err
			}
			doc.ExtraVectors[col.dest] = vec
			if col.field != "" && col.field != col.dest {
				doc.ExtraVectors[col.field] = vec
			}
		}
	}
	result := &QueryResult{Document: doc, HasScore: distance.Valid}
	if distance.Valid {
		result.Score = distance.Float64
	}
	return result, nil
}

type compiledRank struct {
	selectExpr string
	orderExpr  string
	extraWhere string
}

func (s *Store) compileRank(ctx context.Context, namespace string, spec RankSpec, metric string, args *[]interface{}, schema map[string]interface{}) (compiledRank, error) {
	if spec.empty() {
		return compiledRank{selectExpr: "NULL::float8 as distance"}, nil
	}

	raw, orderSuffix, err := s.compileRankRaw(ctx, namespace, spec, metric, args, schema)
	if err != nil {
		return compiledRank{}, err
	}

	selectExpr := raw + " as distance"
	if spec.bm25Only() {
		selectExpr = "-(" + raw + ") as distance"
	}
	if spec.Kind == RankVector && isEuclideanSquaredMetric(metric) {
		selectExpr = "((" + raw + ")^2) as distance"
	}
	if spec.Kind == RankAttribute {
		selectExpr = "NULL::float8 as distance"
	}

	orderExpr := raw
	if orderSuffix != "" {
		orderExpr = raw + " " + orderSuffix
	}
	out := compiledRank{selectExpr: selectExpr, orderExpr: orderExpr}
	if spec.Kind == RankBM25 {
		out.extraWhere = "(" + raw + ") IS DISTINCT FROM 0"
	}
	return out, nil
}

func (s *Store) compileRankRaw(ctx context.Context, namespace string, spec RankSpec, metric string, args *[]interface{}, schema map[string]interface{}) (string, string, error) {
	switch spec.Kind {
	case RankVector:
		op, err := QueryMetricOperator(metric)
		if err != nil {
			return "", "", err
		}
		*args = append(*args, VectorToString(spec.Vector))
		n := len(*args)
		col := vectorColumnSQL(schema, spec.Field)
		expr := fmt.Sprintf("%s %s $%d", col, op, n)
		return expr, "", nil
	case RankBM25:
		cfg := defaultTextConfig
		if ns, err := s.GetNamespace(ctx, namespace); err == nil {
			if fts := parseFTSSchema(ns.Schema[spec.Field]); fts.enabled && fts.textConfig != "" {
				cfg = fts.textConfig
			}
		}
		if err := s.EnsureBM25Index(ctx, namespace, spec.Field, cfg); err != nil {
			return "", "", err
		}
		table := GetNamespaceTableName(s.prefix, namespace)
		idx := GetBM25IndexName(table, spec.Field)
		*args = append(*args, spec.Query, idx)
		n := len(*args)
		expr := fmt.Sprintf("%s <@> to_bm25query($%d, $%d)", jsonTextExpr(spec.Field), n-1, n)
		return expr, "", nil
	case RankAttribute:
		col := filterColumn(spec.Field, false)
		dir := "ASC"
		if spec.Desc {
			dir = "DESC"
		}
		return col, dir, nil
	case RankSum:
		parts, err := s.compileChildRaws(ctx, namespace, spec.Children, metric, args, schema)
		if err != nil {
			return "", "", err
		}
		return "(" + strings.Join(parts, " + ") + ")", "", nil
	case RankMax:
		parts, err := s.compileChildRaws(ctx, namespace, spec.Children, metric, args, schema)
		if err != nil {
			return "", "", err
		}
		return "LEAST(" + strings.Join(parts, ", ") + ")", "", nil
	case RankProduct:
		if len(spec.Children) != 1 {
			return "", "", fmt.Errorf("Product rank_by requires one subquery")
		}
		child, _, err := s.compileRankRaw(ctx, namespace, spec.Children[0], metric, args, schema)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("(%g) * (%s)", spec.Weight, child), "", nil
	default:
		return "", "", fmt.Errorf("unsupported rank kind %q", spec.Kind)
	}
}

func (s *Store) compileChildRaws(ctx context.Context, namespace string, children []RankSpec, metric string, args *[]interface{}, schema map[string]interface{}) ([]string, error) {
	if len(children) == 0 {
		return nil, fmt.Errorf("rank combination requires at least one clause")
	}
	parts := make([]string, 0, len(children))
	for _, child := range children {
		expr, _, err := s.compileRankRaw(ctx, namespace, child, metric, args, schema)
		if err != nil {
			return nil, err
		}
		parts = append(parts, "("+expr+")")
	}
	return parts, nil
}

func (s *Store) queryScoredInProcess(ctx context.Context, namespace string, spec RankSpec, filter Filter, topK int, metric string, exact bool, schema map[string]interface{}) ([]QueryResult, error) {
	tableName := SQLIdent(GetNamespaceTableName(s.prefix, namespace))
	extraSQL, extraCols := extraVectorSelectSQL(schema)
	query := fmt.Sprintf(`SELECT id, vector, attributes, NULL::float8 as distance%s FROM %s`, extraSQL, tableName)
	var args []interface{}
	if filter != nil {
		whereClause, filterArgs, err := buildFilterSQL(filter)
		if err != nil {
			return nil, fmt.Errorf("build filter: %w", err)
		}
		if whereClause != "" {
			query += "\nWHERE " + whereClause
			args = filterArgs
		}
	}
	results, err := s.executeQueryAndParse(ctx, query, args, scanQueryRowWithExtras(extraCols), exact)
	if err != nil {
		return nil, err
	}
	for i := range results {
		score, err := scoreRank(spec, results[i].Document, metric)
		if err != nil {
			return nil, err
		}
		results[i].Score = score
		results[i].HasScore = true
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score < results[j].Score
	})
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func scoreRank(spec RankSpec, doc Document, metric string) (float64, error) {
	switch spec.Kind {
	case RankVector:
		vec := doc.Vector
		if spec.Field != "" && spec.Field != "vector" && doc.ExtraVectors != nil {
			if extra, ok := doc.ExtraVectors[spec.Field]; ok {
				vec = extra
			}
		}
		return VectorDistance(spec.Vector, vec, metric), nil
	case RankSparse:
		return -sparseDot(spec.Sparse, doc.Attributes[spec.Field]), nil
	case RankLate:
		tokens, err := asVectorList(doc.Attributes[spec.Field])
		if err != nil {
			return 0, err
		}
		return maxSim(spec.MultiVector, tokens, metric), nil
	case RankProduct:
		if len(spec.Children) != 1 {
			return 0, fmt.Errorf("Product rank_by requires one subquery")
		}
		child, err := scoreRank(spec.Children[0], doc, metric)
		if err != nil {
			return 0, err
		}
		return spec.Weight * child, nil
	case RankSum:
		var sum float64
		for _, child := range spec.Children {
			v, err := scoreRank(child, doc, metric)
			if err != nil {
				return 0, err
			}
			sum += v
		}
		return sum, nil
	case RankMax:
		best := math.Inf(1)
		for _, child := range spec.Children {
			v, err := scoreRank(child, doc, metric)
			if err != nil {
				return 0, err
			}
			if v < best {
				best = v
			}
		}
		return best, nil
	default:
		return 0, fmt.Errorf("unsupported in-process rank kind %q", spec.Kind)
	}
}

func sparseDot(query map[string]float64, raw interface{}) float64 {
	doc, ok := asFloatMap(raw)
	if !ok || len(query) == 0 {
		return 0
	}
	var sum float64
	for k, qv := range query {
		sum += qv * doc[k]
	}
	return sum
}

func asFloatMap(raw interface{}) (map[string]float64, bool) {
	switch t := raw.(type) {
	case map[string]float64:
		return t, true
	case map[string]float32:
		out := make(map[string]float64, len(t))
		for k, v := range t {
			out[k] = float64(v)
		}
		return out, true
	case map[string]interface{}:
		out := make(map[string]float64, len(t))
		for k, v := range t {
			f, err := coerceFloat64(v)
			if err != nil {
				continue
			}
			out[k] = f
		}
		return out, true
	default:
		return nil, false
	}
}

func maxSim(query, doc [][]float32, metric string) float64 {
	if len(query) == 0 {
		return 0
	}
	if len(doc) == 0 {
		return math.Inf(1)
	}
	var sum float64
	for _, q := range query {
		best := math.Inf(1)
		for _, d := range doc {
			dist := VectorDistance(q, d, metric)
			if dist < best {
				best = dist
			}
		}
		sum += best
	}
	return sum
}

func asVectorList(raw interface{}) ([][]float32, error) {
	switch t := raw.(type) {
	case [][]float32:
		return t, nil
	case []interface{}:
		out := make([][]float32, 0, len(t))
		for _, item := range t {
			vec, err := asFloat32Slice(item)
			if err != nil {
				return nil, err
			}
			out = append(out, vec)
		}
		return out, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("late-interaction attribute must be an array of vectors, got %T", raw)
	}
}

func asFloat32Slice(v interface{}) ([]float32, error) {
	switch t := v.(type) {
	case []float32:
		return t, nil
	case []float64:
		out := make([]float32, len(t))
		for i, x := range t {
			out[i] = float32(x)
		}
		return out, nil
	case []interface{}:
		out := make([]float32, len(t))
		for i, x := range t {
			f, err := coerceFloat64(x)
			if err != nil {
				return nil, err
			}
			out[i] = float32(f)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("vector must be an array, got %T", v)
	}
}

func coerceFloat64(v interface{}) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case json.Number:
		return t.Float64()
	default:
		return 0, fmt.Errorf("not a number: %T", v)
	}
}

func embedDimsForField(schema map[string]interface{}, field string) int {
	for _, spec := range EmbedSpecs(schema) {
		if spec.Field == field || spec.ComputedAttr() == field {
			return spec.Dims
		}
	}
	return 0
}

func attachComputed(results []QueryResult, spec RankSpec, metric string) {
	children := spec.Children
	if len(children) == 0 {
		return
	}
	for i := range results {
		computed := map[string]float64{}
		for j, child := range children {
			v, err := scoreRank(child, results[i].Document, metric)
			if err != nil {
				continue
			}
			name := child.Field
			if name == "" {
				name = fmt.Sprintf("$dist_%d", j)
			}
			computed[name] = v
		}
		if len(computed) > 0 {
			results[i].Computed = computed
		}
	}
}
