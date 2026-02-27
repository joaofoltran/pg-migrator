package schema

import (
	"strings"
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

func TestSplitStatements(t *testing.T) {
	dump := `--
-- PostgreSQL database dump
--

SET statement_timeout = 0;
SET lock_timeout = 0;
SET client_encoding = 'UTF8';

\connect - postgres

CREATE TABLE public.users (
    id serial PRIMARY KEY,
    name text NOT NULL
);

-- comment in the middle
ALTER TABLE public.users OWNER TO postgres;

\.

\. some data

SELECT pg_catalog.set_config('search_path', '', false);
`

	stmts := splitStatements(dump)

	for i, s := range stmts {
		if s == "" {
			t.Errorf("statement %d is empty", i)
		}
		if s[0] == '\\' {
			t.Errorf("statement %d starts with backslash: %q", i, s)
		}
		if s[0] == '-' && s[1] == '-' {
			t.Errorf("statement %d is a comment: %q", i, s)
		}
	}

	expected := []string{
		"SET statement_timeout = 0;",
		"SET lock_timeout = 0;",
		"SET client_encoding = 'UTF8';",
		"CREATE TABLE public.users (\n    id serial PRIMARY KEY,\n    name text NOT NULL\n);",
		"ALTER TABLE public.users OWNER TO postgres;",
		"SELECT pg_catalog.set_config('search_path', '', false);",
	}

	if len(stmts) != len(expected) {
		t.Fatalf("got %d statements, want %d\ngot: %v", len(stmts), len(expected), stmts)
	}

	for i, want := range expected {
		if stmts[i] != want {
			t.Errorf("statement %d:\n  got:  %q\n  want: %q", i, stmts[i], want)
		}
	}
}

func TestSplitStatements_DollarQuotedFunction(t *testing.T) {
	dump := `CREATE FUNCTION public.update_modified() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.modified_at = NOW();
    RETURN NEW;
END;
$$;

ALTER TABLE public.users ADD COLUMN modified_at TIMESTAMPTZ;
`

	stmts := splitStatements(dump)

	if len(stmts) != 2 {
		t.Fatalf("got %d statements, want 2\ngot: %v", len(stmts), stmts)
	}

	if !strings.Contains(stmts[0], "RETURN NEW;") {
		t.Errorf("first statement should contain function body with semicolons, got: %q", stmts[0])
	}
	if !strings.HasPrefix(stmts[0], "CREATE FUNCTION") {
		t.Errorf("first statement should start with CREATE FUNCTION, got: %q", stmts[0])
	}
	if !strings.HasSuffix(stmts[0], "$$;") {
		t.Errorf("first statement should end with $$;, got: %q", stmts[0])
	}
	if !strings.HasPrefix(stmts[1], "ALTER TABLE") {
		t.Errorf("second statement should be ALTER TABLE, got: %q", stmts[1])
	}
}

func TestSplitStatements_NamedDollarTag(t *testing.T) {
	dump := `CREATE FUNCTION public.complex() RETURNS void
    LANGUAGE plpgsql
    AS $body$
DECLARE
    v INT;
BEGIN
    v := 1;
    PERFORM pg_catalog.set_config('x', '', false);
END;
$body$;
`

	stmts := splitStatements(dump)

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1\ngot: %v", len(stmts), stmts)
	}

	if !strings.Contains(stmts[0], "$body$") {
		t.Errorf("statement should contain $body$ tags, got: %q", stmts[0])
	}
	if !strings.Contains(stmts[0], "v := 1;") {
		t.Errorf("statement should preserve semicolons inside body, got: %q", stmts[0])
	}
}

func TestSplitStatements_NestedDollarQuotes(t *testing.T) {
	dump := `CREATE FUNCTION public.exec_dynamic() RETURNS void
    LANGUAGE plpgsql
    AS $outer$
BEGIN
    EXECUTE $inner$SELECT 1; SELECT 2;$inner$;
END;
$outer$;
`

	stmts := splitStatements(dump)

	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1\ngot: %v", len(stmts), stmts)
	}

	if !strings.Contains(stmts[0], "$inner$SELECT 1; SELECT 2;$inner$") {
		t.Errorf("statement should preserve nested dollar-quoted string, got: %q", stmts[0])
	}
}

func TestTrackDollarQuoting(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		inQuote   bool
		tag       string
		wantIn    bool
		wantTag   string
	}{
		{"enter $$", "    AS $$", false, "", true, "$$"},
		{"exit $$", "$$;", true, "$$", false, ""},
		{"enter named", "    AS $body$", false, "", true, "$body$"},
		{"exit named", "$body$;", true, "$body$", false, ""},
		{"no dollar sign", "BEGIN", true, "$$", true, "$$"},
		{"unmatched tag", "$other$;", true, "$$", true, "$$"},
		{"both open and close", "AS $$ RETURN NEW; $$", false, "", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIn, gotTag := trackDollarQuoting(tt.line, tt.inQuote, tt.tag)
			if gotIn != tt.wantIn || gotTag != tt.wantTag {
				t.Errorf("trackDollarQuoting(%q, %v, %q) = (%v, %q), want (%v, %q)",
					tt.line, tt.inQuote, tt.tag, gotIn, gotTag, tt.wantIn, tt.wantTag)
			}
		})
	}
}

func TestParseDollarTag(t *testing.T) {
	tests := []struct {
		line    string
		pos     int
		wantTag string
		wantEnd int
	}{
		{"$$", 0, "$$", 2},
		{"$body$", 0, "$body$", 6},
		{"$fn_1$", 0, "$fn_1$", 6},
		{"AS $$ BEGIN", 3, "$$", 5},
		{"$", 0, "", 0},
		{"$123invalid", 0, "", 0},
		{"no dollar", 0, "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			tag, end := parseDollarTag(tt.line, tt.pos)
			if tag != tt.wantTag || end != tt.wantEnd {
				t.Errorf("parseDollarTag(%q, %d) = (%q, %d), want (%q, %d)",
					tt.line, tt.pos, tag, end, tt.wantTag, tt.wantEnd)
			}
		})
	}
}
