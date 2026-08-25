package main

import "testing"

func TestMetricsHistogram(t *testing.T) {
	m := &metrics{}
	m.observe(3, 200)
	m.observe(50, 404)
	if m.requests.Load() != 2 {
		t.Fatalf("requests=%d", m.requests.Load())
	}
	if m.latencyBuckets[0].Load() != 1 {
		t.Fatalf("le=5 got %d", m.latencyBuckets[0].Load())
	}
	if m.latencyBuckets[len(latencyBucketBounds)].Load() != 2 {
		t.Fatalf("+Inf got %d", m.latencyBuckets[len(latencyBucketBounds)].Load())
	}
	if m.status4.Load() != 1 || m.status5.Load() != 0 {
		t.Fatalf("4xx=%d 5xx=%d", m.status4.Load(), m.status5.Load())
	}
	if m.latencySum.Load() != 53 {
		t.Fatalf("sum=%d", m.latencySum.Load())
	}
}
