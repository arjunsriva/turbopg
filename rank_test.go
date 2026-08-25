package turbopg

import "testing"

func TestRankSpecBM25Only(t *testing.T) {
	bm25 := RankSpec{Kind: RankBM25, Field: "content", Query: "q"}
	if !bm25.bm25Only() {
		t.Fatal("expected bm25-only")
	}
	sum := RankSpec{Kind: RankSum, Children: []RankSpec{bm25, {Kind: RankBM25, Field: "title", Query: "q"}}}
	if !sum.bm25Only() {
		t.Fatal("sum of bm25 should be bm25-only")
	}
	mixed := RankSpec{Kind: RankSum, Children: []RankSpec{bm25, {Kind: RankVector, Vector: []float32{1}}}}
	if mixed.bm25Only() {
		t.Fatal("mixed sum is not bm25-only")
	}
}
