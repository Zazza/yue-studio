package mcp

import "testing"

// Спецификация «характер исполнения»: инструмент submit передаёт temperature и
// cfg в SubmitParams каждой джобы (в том числе в веере n); без аргументов — 0.

func TestCharacterSubmitPassesValues(t *testing.T) {
	s, fake := newTestServer(t)
	out, ok := call(t, s, "submit", map[string]any{
		"style": "punk", "lyrics": "[Instrumental]", "temperature": 1.15, "cfg": 2.5,
	})
	if !ok {
		t.Fatalf("submit failed: %s", out)
	}
	if len(fake.submitted) != 1 {
		t.Fatalf("submitted %d", len(fake.submitted))
	}
	if p := fake.submitted[0]; p.Temperature != 1.15 || p.Cfg != 2.5 {
		t.Fatalf("params %+v: want Temperature=1.15 Cfg=2.5", p)
	}
}

func TestCharacterSubmitFanEachJob(t *testing.T) {
	s, fake := newTestServer(t)
	out, ok := call(t, s, "submit", map[string]any{
		"style": "punk", "lyrics": "[Instrumental]", "seed": 10, "n": 3,
		"temperature": 1.15, "cfg": 2.5,
	})
	if !ok {
		t.Fatalf("submit failed: %s", out)
	}
	if len(fake.submitted) != 3 {
		t.Fatalf("submitted %d", len(fake.submitted))
	}
	for i, p := range fake.submitted {
		if p.Temperature != 1.15 || p.Cfg != 2.5 {
			t.Fatalf("job %d params %+v: want Temperature=1.15 Cfg=2.5", i, p)
		}
	}
}

func TestCharacterSubmitDefaultZero(t *testing.T) {
	s, fake := newTestServer(t)
	out, ok := call(t, s, "submit", map[string]any{"style": "punk", "lyrics": "[Instrumental]"})
	if !ok {
		t.Fatalf("submit failed: %s", out)
	}
	if p := fake.submitted[0]; p.Temperature != 0 || p.Cfg != 0 {
		t.Fatalf("params %+v: want 0/0", p)
	}
}

func TestCharacterSubmitSchemaHasFields(t *testing.T) {
	s, _ := newTestServer(t)
	s.mu.RLock()
	tool := s.tools["submit"]
	s.mu.RUnlock()
	props, _ := tool.InputSchema["properties"].(map[string]any)
	for _, k := range []string{"temperature", "cfg"} {
		if _, ok := props[k]; !ok {
			t.Fatalf("submit schema lacks %q", k)
		}
	}
}
