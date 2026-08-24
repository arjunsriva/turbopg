package turbopg

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// VectorToString converts a vector to the pgvector string format
func VectorToString(vector []float32) string {
	return fmt.Sprintf("[%s]", joinFloat32s(vector, ","))
}

func vectorArg(vector []float32) interface{} {
	if len(vector) == 0 {
		return nil
	}
	return VectorToString(vector)
}

// joinFloat32s joins float32 values with a separator
func joinFloat32s(values []float32, sep string) string {
	if len(values) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%g", values[0]))
	for _, v := range values[1:] {
		b.WriteString(sep)
		b.WriteString(fmt.Sprintf("%g", v))
	}
	return b.String()
}

// StringToVector converts a pgvector string format back to a vector
func StringToVector(vectorStr string) ([]float32, error) {
	vectorStr = strings.Trim(vectorStr, "[]")
	if vectorStr == "" {
		return nil, nil
	}

	parts := strings.Split(vectorStr, ",")
	vector := make([]float32, len(parts))
	for i, p := range parts {
		val, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("parse vector value: %w", err)
		}
		vector[i] = float32(val)
	}
	return vector, nil
}

func isEuclideanSquaredMetric(metric string) bool {
	switch metric {
	case "euclidean_squared", "euclidean_squared_distance":
		return true
	default:
		return false
	}
}

func VectorDistance(a, b []float32, metric string) float64 {
	if isEuclideanSquaredMetric(metric) {
		return l2Squared(a, b)
	}
	switch metric {
	case "euclidean", "l2":
		return math.Sqrt(l2Squared(a, b))
	default:
		return cosineDistance(a, b)
	}
}

func cosineDistance(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 1
	}
	sim := dot / (math.Sqrt(na) * math.Sqrt(nb))
	if sim > 1 {
		sim = 1
	}
	if sim < -1 {
		sim = -1
	}
	return 1 - sim
}

func l2Squared(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var sum float64
	for i := 0; i < n; i++ {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	return sum
}
