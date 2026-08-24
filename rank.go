package turbopg

// RankKind identifies how Query ranks documents.
type RankKind string

const (
	RankNone      RankKind = ""
	RankVector    RankKind = "vector"
	RankBM25      RankKind = "bm25"
	RankAttribute RankKind = "attr"
	RankSum       RankKind = "sum"
	RankProduct   RankKind = "product"
	RankMax       RankKind = "max"
	RankSparse    RankKind = "sparse"
	RankLate      RankKind = "late"
)

// RankSpec is a TurboPuffer-style rank_by expression.
type RankSpec struct {
	Kind     RankKind
	Field    string
	Vector   []float32
	Query    string
	Desc     bool
	Weight   float64
	Children []RankSpec
	// Exact is true for kNN (exhaustive) ranking. ANN leaves it false.
	Exact bool
	// Sparse is the query vector for SparseKNN ({}f16 attributes).
	Sparse map[string]float64
	// MultiVector is the query token vectors for late-interaction (ColBERT) ranking.
	MultiVector [][]float32
	// EmbedQuery is set when rank_by uses ["Embed", text] instead of a vector.
	EmbedQuery string
	// EmbedModel overrides the schema embed model for this query.
	EmbedModel string
}

func (r RankSpec) empty() bool {
	return r.Kind == RankNone && len(r.Vector) == 0 && len(r.Children) == 0 && len(r.Sparse) == 0 && len(r.MultiVector) == 0 && r.EmbedQuery == ""
}

func (r RankSpec) inProcess() bool {
	switch r.Kind {
	case RankSparse, RankLate:
		return true
	case RankSum, RankMax, RankProduct:
		for _, c := range r.Children {
			if c.inProcess() {
				return true
			}
		}
	}
	return false
}

func (r RankSpec) bm25Only() bool {
	switch r.Kind {
	case RankBM25:
		return true
	case RankSum, RankMax:
		if len(r.Children) == 0 {
			return false
		}
		for _, c := range r.Children {
			if !c.bm25Only() {
				return false
			}
		}
		return true
	case RankProduct:
		return len(r.Children) == 1 && r.Children[0].bm25Only()
	default:
		return false
	}
}
