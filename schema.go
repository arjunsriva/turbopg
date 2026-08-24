package turbopg

import (
	"encoding/json"
	"strconv"
)

// AttributeDef is one TurboPuffer-style schema field after parsing the catalog JSON.
type AttributeDef struct {
	Type           string
	Filterable     *bool
	FullTextSearch interface{}
	Embed          *EmbedConfig
	Regex          bool
	Glob           bool
	Fuzzy          bool
}

// EmbedConfig is the schema `embed` object (or string model name).
type EmbedConfig struct {
	Model     string
	Dims      int
	Attribute string
	Disabled  bool
}

// EmbedSpec is an enabled embedding field used on write and query.
type EmbedSpec struct {
	Field     string
	Model     string
	Dims      int
	Attribute string
	Disabled  bool
}

// ComputedAttr is the attribute that stores the embedding, or "" when the
// vector column itself is the destination (`attribute: "vector"`).
func (s EmbedSpec) ComputedAttr() string {
	if s.Attribute == "vector" {
		return ""
	}
	if s.Attribute != "" {
		return s.Attribute
	}
	return "embed_" + s.Field
}

// ParseAttributeDef interprets a schema value (string type, bool FTS shorthand, or object).
func ParseAttributeDef(def interface{}) AttributeDef {
	switch t := def.(type) {
	case bool:
		return AttributeDef{FullTextSearch: t}
	case string:
		return AttributeDef{Type: t}
	case map[string]interface{}:
		d := AttributeDef{}
		if typ, ok := t["type"].(string); ok {
			d.Type = typ
		}
		if f, ok := t["filterable"].(bool); ok {
			d.Filterable = &f
		}
		if fts, ok := t["full_text_search"]; ok {
			d.FullTextSearch = fts
		}
		if embed, exists := t["embed"]; exists {
			d.Embed = parseEmbedConfig(embed)
		}
		if v, ok := t["regex"].(bool); ok {
			d.Regex = v
		}
		if v, ok := t["glob"].(bool); ok {
			d.Glob = v
		}
		if v, ok := t["fuzzy"].(bool); ok {
			d.Fuzzy = v
		}
		return d
	default:
		return AttributeDef{}
	}
}

func parseEmbedConfig(embed interface{}) *EmbedConfig {
	if embed == nil {
		return &EmbedConfig{Disabled: true}
	}
	switch t := embed.(type) {
	case string:
		return &EmbedConfig{Model: t}
	case map[string]interface{}:
		return &EmbedConfig{
			Model:     stringFromAny(t["model"]),
			Attribute: stringFromAny(t["attribute"]),
			Dims:      intFromAny(t["dims"]),
		}
	default:
		return nil
	}
}

// IsFilterable reports whether a schema field should have a btree filter index.
// Non-object definitions are never filter-indexed.
func (d AttributeDef) IsFilterable(raw interface{}) bool {
	if _, ok := raw.(map[string]interface{}); !ok {
		return false
	}
	if d.Filterable != nil {
		return *d.Filterable
	}
	if d.FullTextSearch == nil {
		return true
	}
	switch v := d.FullTextSearch.(type) {
	case bool:
		return !v
	default:
		return v == nil
	}
}

// EmbedSpecs returns enabled embedding fields from a namespace schema.
func EmbedSpecs(schema map[string]interface{}) []EmbedSpec {
	if schema == nil {
		return nil
	}
	out := make([]EmbedSpec, 0)
	for field, def := range schema {
		if field == "id" || field == "vector" {
			continue
		}
		spec, ok := ParseEmbedSpec(field, def)
		if !ok || spec.Disabled || spec.Model == "" {
			continue
		}
		out = append(out, spec)
	}
	return out
}

// ParseEmbedSpec reads one field's embed config. ok is false when embed is absent.
func ParseEmbedSpec(field string, def interface{}) (EmbedSpec, bool) {
	d := ParseAttributeDef(def)
	if d.Embed == nil {
		return EmbedSpec{}, false
	}
	return EmbedSpec{
		Field:     field,
		Model:     d.Embed.Model,
		Dims:      d.Embed.Dims,
		Attribute: d.Embed.Attribute,
		Disabled:  d.Embed.Disabled,
	}, true
}

func intFromAny(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

func stringFromAny(v interface{}) string {
	s, _ := v.(string)
	return s
}
