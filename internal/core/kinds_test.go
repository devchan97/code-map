package core

import (
	"encoding/json"
	"testing"
)

func TestParseSymbolKind(t *testing.T) {
	tests := []struct {
		in      string
		want    SymbolKind
		wantErr bool
	}{
		{"function", SymbolFunction, false},
		{"method", SymbolMethod, false},
		{"class", SymbolClass, false},
		{"variable", SymbolVariable, false},
		{"import", SymbolImport, false},
		{"constant", SymbolConstant, false},
		{"Function", "", true},
		{"", "", true},
		{"unknown", "", true},
	}
	for _, tt := range tests {
		got, err := ParseSymbolKind(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseSymbolKind(%q) err=%v wantErr=%v", tt.in, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("ParseSymbolKind(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseScope(t *testing.T) {
	for _, s := range []Scope{ScopeGlobal, ScopeClass, ScopeLocal, ScopeParam} {
		got, err := ParseScope(string(s))
		if err != nil || got != s {
			t.Errorf("ParseScope(%q) = (%q, %v); want (%q, nil)", s, got, err, s)
		}
	}
	if _, err := ParseScope("module"); err == nil {
		t.Error("ParseScope(\"module\") should error")
	}
}

func TestParseEdgeKind(t *testing.T) {
	for _, k := range []EdgeKind{EdgeCall, EdgeReference, EdgeInherit, EdgeImport} {
		got, err := ParseEdgeKind(string(k))
		if err != nil || got != k {
			t.Errorf("ParseEdgeKind(%q) = (%q, %v); want (%q, nil)", k, got, err, k)
		}
	}
	if _, err := ParseEdgeKind("invoke"); err == nil {
		t.Error("ParseEdgeKind(\"invoke\") should error")
	}
}

func TestSymbolKindJSON_Roundtrip(t *testing.T) {
	for _, in := range []SymbolKind{SymbolFunction, SymbolMethod, SymbolClass, SymbolVariable, SymbolImport, SymbolConstant} {
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("Marshal(%q): %v", in, err)
		}
		want := `"` + string(in) + `"`
		if string(data) != want {
			t.Errorf("Marshal(%q) = %s, want %s", in, data, want)
		}
		var out SymbolKind
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("Unmarshal(%s): %v", data, err)
		}
		if out != in {
			t.Errorf("roundtrip got %q want %q", out, in)
		}
	}
}

func TestSymbolKindJSON_RejectsUnknown(t *testing.T) {
	var k SymbolKind
	if err := json.Unmarshal([]byte(`"bogus"`), &k); err == nil {
		t.Error("expected error on bogus SymbolKind")
	}
}

func TestScopeJSON_Roundtrip(t *testing.T) {
	for _, in := range []Scope{ScopeGlobal, ScopeClass, ScopeLocal, ScopeParam} {
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var out Scope
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		if out != in {
			t.Errorf("Scope roundtrip got %q want %q", out, in)
		}
	}
}

func TestEdgeKindJSON_Roundtrip(t *testing.T) {
	for _, in := range []EdgeKind{EdgeCall, EdgeReference, EdgeInherit, EdgeImport} {
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var out EdgeKind
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		if out != in {
			t.Errorf("EdgeKind roundtrip got %q want %q", out, in)
		}
	}
}

func TestKindString(t *testing.T) {
	if SymbolFunction.String() != "function" {
		t.Errorf("SymbolFunction.String() = %q", SymbolFunction.String())
	}
	if ScopeLocal.String() != "local" {
		t.Errorf("ScopeLocal.String() = %q", ScopeLocal.String())
	}
	if EdgeCall.String() != "call" {
		t.Errorf("EdgeCall.String() = %q", EdgeCall.String())
	}
}
