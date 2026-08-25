package turbopg

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func coerceTypedAttributes(schema map[string]interface{}, docs []Document) error {
	if len(schema) == 0 {
		return nil
	}
	for i := range docs {
		if err := coerceDocTypes(schema, &docs[i]); err != nil {
			return err
		}
	}
	return nil
}

func coerceDocTypes(schema map[string]interface{}, doc *Document) error {
	if doc.Attributes == nil {
		return nil
	}
	for field, raw := range schema {
		if field == "id" || field == "vector" {
			continue
		}
		val, ok := doc.Attributes[field]
		if !ok || val == nil {
			continue
		}
		def := ParseAttributeDef(raw)
		switch strings.ToLower(def.Type) {
		case "uuid":
			s := strings.TrimSpace(fmt.Sprint(val))
			if !uuidRE.MatchString(s) {
				return InvalidInputf("attribute %q is not a uuid", field)
			}
			doc.Attributes[field] = strings.ToLower(s)
		case "datetime", "date":
			s, err := parseDateTime(val)
			if err != nil {
				return InvalidInputf("attribute %q is not a datetime: %v", field, err)
			}
			doc.Attributes[field] = s
		}
	}
	return nil
}

func parseDateTime(v interface{}) (string, error) {
	switch t := v.(type) {
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano), nil
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02"} {
			if parsed, err := time.Parse(layout, s); err == nil {
				if layout == "2006-01-02" {
					return parsed.Format("2006-01-02"), nil
				}
				return parsed.UTC().Format(time.RFC3339Nano), nil
			}
		}
		return "", fmt.Errorf("%q", s)
	}
}
