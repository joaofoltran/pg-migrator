package snapshot

import (
	"testing"
)

func TestTableInfo_QualifiedName(t *testing.T) {
	tests := []struct {
		schema string
		name   string
		want   string
	}{
		{"public", "users", "users"},
		{"", "users", "users"},
		{"myschema", "orders", "myschema.orders"},
	}

	for _, tt := range tests {
		ti := TableInfo{Schema: tt.schema, Name: tt.name}
		got := ti.QualifiedName()
		if got != tt.want {
			t.Errorf("QualifiedName(%q, %q) = %q, want %q", tt.schema, tt.name, got, tt.want)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"users", `"users"`},
		{"order", `"order"`},
		{`my"table`, `"my""table"`},
		{"", `""`},
	}
	for _, tt := range tests {
		got := quoteIdent(tt.input)
		if got != tt.want {
			t.Errorf("quoteIdent(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestQuoteQualifiedName(t *testing.T) {
	tests := []struct {
		schema string
		table  string
		want   string
	}{
		{"public", "users", `"users"`},
		{"", "users", `"users"`},
		{"myschema", "orders", `"myschema"."orders"`},
		{"my schema", "my table", `"my schema"."my table"`},
	}
	for _, tt := range tests {
		got := quoteQualifiedName(tt.schema, tt.table)
		if got != tt.want {
			t.Errorf("quoteQualifiedName(%q, %q) = %q, want %q", tt.schema, tt.table, got, tt.want)
		}
	}
}
