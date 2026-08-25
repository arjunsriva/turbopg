package turbopg

import "testing"

func TestGetVectorColumnName(t *testing.T) {
	a := GetVectorColumnName("title_vec")
	b := GetVectorColumnName("title_vec")
	if a != b || a == "" || len(a) > 63 {
		t.Fatalf("col=%s", a)
	}
	if GetVectorColumnName("other") == a {
		t.Fatal("expected distinct columns")
	}
}

func TestParseRegexGlobFlags(t *testing.T) {
	d := ParseAttributeDef(map[string]interface{}{"type": "string", "regex": true, "glob": true})
	if !d.Regex || !d.Glob {
		t.Fatalf("%+v", d)
	}
}

func TestVectorColumnSQL(t *testing.T) {
	if vectorColumnSQL(nil, "") != "vector" || vectorColumnSQL(nil, "vector") != "vector" {
		t.Fatal("primary vector column")
	}
	schema := map[string]interface{}{
		"text": map[string]interface{}{
			"type": "string",
			"embed": map[string]interface{}{
				"model": "voyage/voyage-4",
				"dims":  3,
			},
		},
	}
	want := SQLIdent(GetVectorColumnName("embed_text"))
	if got := vectorColumnSQL(schema, "text"); got != want {
		t.Fatalf("field key col=%s want=%s", got, want)
	}
	if got := vectorColumnSQL(schema, "embed_text"); got != want {
		t.Fatalf("dest col=%s want=%s", got, want)
	}
}
