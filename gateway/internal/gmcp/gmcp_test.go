package gmcp

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name, in  string
		pkg, data string
		bad       bool
	}{
		{"object", `Char.Vitals {"hp": 380,  "maxhp":412}`, "Char.Vitals", `{"hp":380,"maxhp":412}`, false},
		{"no payload", `Core.Ping`, "Core.Ping", ``, false},
		{"trailing space", "Core.Ping ", "Core.Ping", ``, false},
		{"array", `Core.Supports.Set ["Char 1","Room 1"]`, "Core.Supports.Set", `["Char 1","Room 1"]`, false},
		{"string scalar", `Room.Note "hi there"`, "Room.Note", `"hi there"`, false},
		{"number", `X.Y 42`, "X.Y", `42`, false},
		{"unknown pkg is fine", `Some.Future.Package {"a":{"b":[1,2,3]}}`, "Some.Future.Package", `{"a":{"b":[1,2,3]}}`, false},
		{"bad json", `Char.Vitals {"hp":`, "Char.Vitals", ``, true},
		{"empty", ``, ``, ``, true},
		{"leading space", ` Char.Vitals {}`, ``, ``, true},
		{"bad name chars", `Ch ar {}`, `Ch`, ``, true},
		{"injection-looking name", "#send{x}; {}", ``, ``, true},
		{"double dot", `A..B {}`, ``, ``, true},
		{"tab separator", "A.B\t{\"x\":1}", "A.B", `{"x":1}`, false},
	}
	for _, c := range cases {
		m := Parse([]byte(c.in))
		if m.Malformed != c.bad {
			t.Errorf("%s: malformed=%v (%s)", c.name, m.Malformed, m.Error)
		}
		if !c.bad && (m.Package != c.pkg || string(m.Data) != c.data) {
			t.Errorf("%s: got %q %q want %q %q", c.name, m.Package, m.Data, c.pkg, c.data)
		}
		if c.bad && m.Raw == "" && c.in != "" {
			t.Errorf("%s: malformed message lost raw payload", c.name)
		}
	}
}

func TestParseBoundsAndUTF8(t *testing.T) {
	m := Parse([]byte(strings.Repeat("a", MaxPackageLen+1) + " {}"))
	if !m.Malformed {
		t.Fatal("overlong package accepted")
	}
	m = Parse([]byte("A.B {" + strings.Repeat("x", 10000)))
	if !m.Malformed || len(m.Raw) > MaxRawLen+8 {
		t.Fatalf("raw not truncated: %d", len(m.Raw))
	}
	m = Parse([]byte("A.B \xff\xfe"))
	if !m.Malformed || !strings.Contains(m.Raw, "�") {
		t.Fatalf("invalid UTF-8 not sanitised: %q", m.Raw)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"A.B {}", "", "x", "A.B [", "\xff"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		m := Parse(b)
		if !m.Malformed && m.Package == "" {
			t.Fatal("well-formed message without package")
		}
	})
}
