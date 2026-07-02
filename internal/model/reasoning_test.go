package model

import "testing"

func TestLookupReserve_KnownReasoningModel(t *testing.T) {
	cases := []struct {
		model string
		want  int
	}{
		{"qwen3:14b", 8192},
		{"Qwen3-32B-Instruct", 8192},
		{"qwq-32b", 8192},
		{"deepseek-r1:7b", 8192},
	}
	for _, c := range cases {
		got, ok := LookupReserve(c.model)
		if !ok {
			t.Errorf("LookupReserve(%q): ok = false, want true", c.model)
			continue
		}
		if got != c.want {
			t.Errorf("LookupReserve(%q) = %d, want %d", c.model, got, c.want)
		}
	}
}

func TestLookupReserve_UnknownModel(t *testing.T) {
	got, ok := LookupReserve("llama3:8b")
	if ok {
		t.Errorf("LookupReserve(llama3:8b): ok = true, want false (got %d)", got)
	}
}
