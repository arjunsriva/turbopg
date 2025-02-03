package turbopg

import (
	"fmt"
	"strconv"
	"strings"
)

// VectorToString converts a vector to the pgvector string format
func VectorToString(vector []float32) string {
	return fmt.Sprintf("[%s]", joinFloat32s(vector, ","))
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
