package replay

import (
	"testing"

	"github.com/jfoltran/migrator/internal/migration/stream"
)

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"users", `"users"`},
		{"order", `"order"`},
		{`my"table`, `"my""table"`},
		{"", `""`},
		{"CamelCase", `"CamelCase"`},
	}
	for _, tt := range tests {
		got := quoteIdent(tt.input)
		if got != tt.want {
			t.Errorf("quoteIdent(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestQualifiedName(t *testing.T) {
	tests := []struct {
		namespace string
		table     string
		want      string
	}{
		{"public", "users", `"users"`},
		{"", "users", `"users"`},
		{"myschema", "users", `"myschema"."users"`},
		{"my schema", "my table", `"my schema"."my table"`},
	}
	for _, tt := range tests {
		got := qualifiedName(tt.namespace, tt.table)
		if got != tt.want {
			t.Errorf("qualifiedName(%q, %q) = %q, want %q", tt.namespace, tt.table, got, tt.want)
		}
	}
}

func TestBuildInsertParts(t *testing.T) {
	a := &Applier{relations: make(map[uint32]*stream.RelationMessage)}

	tuple := &stream.TupleData{
		Columns: []stream.Column{
			{Name: "id", Value: []byte("1")},
			{Name: "name", Value: []byte("alice")},
		},
	}

	cols, vals, placeholders := a.buildInsertParts(tuple)

	if len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(cols))
	}
	if cols[0] != `"id"` || cols[1] != `"name"` {
		t.Errorf("cols = %v", cols)
	}
	if vals[0] != "1" || vals[1] != "alice" {
		t.Errorf("vals = %v", vals)
	}
	if placeholders[0] != "$1" || placeholders[1] != "$2" {
		t.Errorf("placeholders = %v", placeholders)
	}
}

func TestBuildSetClauses(t *testing.T) {
	a := &Applier{relations: make(map[uint32]*stream.RelationMessage)}

	tuple := &stream.TupleData{
		Columns: []stream.Column{
			{Name: "name", Value: []byte("bob")},
			{Name: "email", Value: []byte("bob@example.com")},
		},
	}

	clauses, vals := a.buildSetClauses(tuple)
	if len(clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d", len(clauses))
	}
	if clauses[0] != `"name" = $1` {
		t.Errorf("clause[0] = %q", clauses[0])
	}
	if clauses[1] != `"email" = $2` {
		t.Errorf("clause[1] = %q", clauses[1])
	}
	if vals[0] != "bob" || vals[1] != "bob@example.com" {
		t.Errorf("vals = %v", vals)
	}
}

func TestBuildWhereClauses_FromOldTuple(t *testing.T) {
	a := &Applier{relations: make(map[uint32]*stream.RelationMessage)}

	m := &stream.ChangeMessage{
		OldTuple: &stream.TupleData{
			Columns: []stream.Column{
				{Name: "id", Value: []byte("42")},
			},
		},
		NewTuple: &stream.TupleData{
			Columns: []stream.Column{
				{Name: "id", Value: []byte("42")},
				{Name: "name", Value: []byte("alice")},
			},
		},
	}

	clauses, vals := a.buildWhereClauses(m, nil, 2)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 WHERE clause, got %d", len(clauses))
	}
	if clauses[0] != `"id" = $3` {
		t.Errorf("clause = %q, want %q", clauses[0], `"id" = $3`)
	}
	if vals[0] != "42" {
		t.Errorf("val = %v", vals[0])
	}
}

func TestBuildWhereClauses_FallbackToNewTuple(t *testing.T) {
	a := &Applier{relations: make(map[uint32]*stream.RelationMessage)}

	m := &stream.ChangeMessage{
		OldTuple: nil,
		NewTuple: &stream.TupleData{
			Columns: []stream.Column{
				{Name: "id", Value: []byte("7")},
			},
		},
	}

	clauses, vals := a.buildWhereClauses(m, nil, 0)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 WHERE clause, got %d", len(clauses))
	}
	if clauses[0] != `"id" = $1` {
		t.Errorf("clause = %q", clauses[0])
	}
	if vals[0] != "7" {
		t.Errorf("val = %v", vals[0])
	}
}

func TestBuildWhereClauses_BothNil(t *testing.T) {
	a := &Applier{relations: make(map[uint32]*stream.RelationMessage)}
	m := &stream.ChangeMessage{}

	clauses, vals := a.buildWhereClauses(m, nil, 0)
	if len(clauses) != 0 || len(vals) != 0 {
		t.Errorf("expected empty results, got clauses=%v vals=%v", clauses, vals)
	}
}
