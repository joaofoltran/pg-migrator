package replay

import (
	"testing"

	"github.com/jfoltran/pgmanager/internal/migration/stream"
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

func TestInsertBatch_Add(t *testing.T) {
	var b insertBatch
	b.reset("public", "users")

	msg := &stream.ChangeMessage{
		Namespace: "public",
		Table:     "users",
		Op:        stream.OpInsert,
		NewTuple: &stream.TupleData{
			Columns: []stream.Column{
				{Name: "id", Value: []byte("1")},
				{Name: "name", Value: []byte("alice")},
			},
		},
	}

	b.add(msg)
	if b.len() != 1 {
		t.Fatalf("expected 1 row, got %d", b.len())
	}
	if len(b.cols) != 2 {
		t.Fatalf("expected 2 cols, got %d", len(b.cols))
	}
	if b.cols[0] != "id" || b.cols[1] != "name" {
		t.Errorf("cols = %v", b.cols)
	}
	if b.rows[0][0] != "1" || b.rows[0][1] != "alice" {
		t.Errorf("row = %v", b.rows[0])
	}
}

func TestInsertBatch_Matches(t *testing.T) {
	var b insertBatch
	b.reset("public", "users")

	same := &stream.ChangeMessage{Namespace: "public", Table: "users"}
	diff := &stream.ChangeMessage{Namespace: "public", Table: "orders"}

	if !b.matches(same) {
		t.Error("expected match for same table")
	}
	if b.matches(diff) {
		t.Error("expected no match for different table")
	}
}

func TestInsertBatch_NilTuple(t *testing.T) {
	var b insertBatch
	b.reset("public", "users")
	b.add(&stream.ChangeMessage{NewTuple: nil})
	if b.len() != 0 {
		t.Errorf("expected 0 rows for nil tuple, got %d", b.len())
	}
}
