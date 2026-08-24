package turbopg

import "testing"

func TestVectorRoundTrip(t *testing.T) {
	s := VectorToString([]float32{1, 2.5})
	got, err := StringToVector(s)
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	empty, err := StringToVector("[]")
	if err != nil || empty != nil {
		t.Fatalf("empty=%v err=%v", empty, err)
	}
	if VectorToString(nil) != "[]" {
		t.Fatal(VectorToString(nil))
	}
	if _, err := StringToVector("[nope]"); err == nil {
		t.Fatal("expected parse error")
	}
}
