package schema

import (
	"testing"
)

func TestSchemaDiff_HasDifferences(t *testing.T) {
	tests := []struct {
		name string
		diff SchemaDiff
		want bool
	}{
		{"empty", SchemaDiff{}, false},
		{"missing tables", SchemaDiff{MissingTables: []string{"t1"}}, true},
		{"extra tables", SchemaDiff{ExtraTables: []string{"t2"}}, true},
		{"column diffs", SchemaDiff{ColumnDiffs: []ColumnDiff{{Table: "t", Column: "c"}}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.diff.HasDifferences(); got != tt.want {
				t.Errorf("HasDifferences() = %v, want %v", got, tt.want)
			}
		})
	}
}
