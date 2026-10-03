package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeHash — подменённый подсчёт отпечатка: считает вызовы, отдаёт текущее значение.
type fakeHash struct {
	calls int
	value string
	err   error
}

func (f *fakeHash) fn(root string) (string, error) {
	f.calls++
	return f.value, f.err
}

// fakeClock — управляемое время.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newChecker(build string, h *fakeHash, c *fakeClock) *staleChecker {
	return &staleChecker{buildHash: build, srcDir: "/src", hash: h.fn, now: c.now}
}

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)} }

func TestStaleNilChecker(t *testing.T) {
	var c *staleChecker
	if w := c.warning(); w != "" {
		t.Fatalf("nil checker: %q", w)
	}
}

func TestStaleNoBuildInfoSkipsHash(t *testing.T) {
	cases := map[string]*staleChecker{}
	h1, h2 := &fakeHash{value: "bbbbbbbbbbbb"}, &fakeHash{value: "bbbbbbbbbbbb"}
	clk := newClock()
	cases["empty buildHash"] = &staleChecker{buildHash: "", srcDir: "/src", hash: h1.fn, now: clk.now}
	cases["empty srcDir"] = &staleChecker{buildHash: "aaaaaaaaaaaa", srcDir: "", hash: h2.fn, now: clk.now}
	for name, c := range cases {
		if w := c.warning(); w != "" {
			t.Fatalf("%s: warning %q", name, w)
		}
	}
	if h1.calls != 0 || h2.calls != 0 {
		t.Fatalf("hash called: %d, %d", h1.calls, h2.calls)
	}
}

func TestStaleFresh(t *testing.T) {
	h := &fakeHash{value: "aaaaaaaaaaaa"}
	if w := newChecker("aaaaaaaaaaaa", h, newClock()).warning(); w != "" {
		t.Fatalf("fresh build: %q", w)
	}
	if h.calls != 1 {
		t.Fatalf("hash calls = %d, want 1", h.calls)
	}
}

func TestStaleWarns(t *testing.T) {
	h := &fakeHash{value: "bbbbbbbbbbbb"}
	w := newChecker("aaaaaaaaaaaa", h, newClock()).warning()
	if w == "" {
		t.Fatal("stale build: no warning")
	}
	if !strings.Contains(w, "make mcp") || !strings.Contains(w, "/mcp") {
		t.Fatalf("warning must mention make mcp and /mcp: %q", w)
	}
}

func TestStaleHashErrorIsSilent(t *testing.T) {
	h := &fakeHash{err: errors.New("no such dir")}
	if w := newChecker("aaaaaaaaaaaa", h, newClock()).warning(); w != "" {
		t.Fatalf("hash error must be silent: %q", w)
	}
}

func TestStaleCachingAndRecheck(t *testing.T) {
	h := &fakeHash{value: "bbbbbbbbbbbb"}
	clk := newClock()
	c := newChecker("aaaaaaaaaaaa", h, clk)

	if c.warning() == "" {
		t.Fatal("first call: want warning")
	}
	if h.calls != 1 {
		t.Fatalf("first call: hash calls = %d", h.calls)
	}

	// Исходники «починили», но 30 минут не прошло — результат из кэша, хэш не считается.
	h.value = "aaaaaaaaaaaa"
	t0 := clk.t
	for _, d := range []time.Duration{time.Second, 10 * time.Minute, 29*time.Minute + 59*time.Second} {
		clk.t = t0.Add(d)
		if c.warning() == "" {
			t.Fatalf("+%v: cached stale result lost", d)
		}
	}
	if h.calls != 1 {
		t.Fatalf("within 30 min: hash calls = %d, want 1", h.calls)
	}

	// Через 30 минут — пересчёт, предупреждение пропадает.
	clk.t = t0.Add(30 * time.Minute)
	if w := c.warning(); w != "" {
		t.Fatalf("after 30 min with fresh sources: %q", w)
	}
	if h.calls != 2 {
		t.Fatalf("after 30 min: hash calls = %d, want 2", h.calls)
	}

	// И обратно: снова устарел — через следующие 30 минут предупреждение возвращается.
	h.value = "cccccccccccc"
	clk.t = clk.t.Add(time.Minute)
	if w := c.warning(); w != "" {
		t.Fatalf("cached fresh result expected, got %q", w)
	}
	clk.t = clk.t.Add(30 * time.Minute)
	if c.warning() == "" {
		t.Fatal("after another 30 min: want warning again")
	}
	if h.calls != 3 {
		t.Fatalf("hash calls = %d, want 3", h.calls)
	}
}

// callTool — tools/call через диспетчер, как его видит клиент MCP.
func callTool(t *testing.T, s *Server, name string) callToolResult {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": map[string]any{}})
	resp := s.dispatch(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params})
	if resp.Error != nil {
		t.Fatalf("rpc error: %+v", resp.Error)
	}
	res, ok := resp.Result.(callToolResult)
	if !ok {
		t.Fatalf("result type %T", resp.Result)
	}
	if len(res.Content) == 0 {
		t.Fatal("empty content")
	}
	return res
}

func registerProbeTools(s *Server) {
	s.Register(Tool{Name: "probe_ok", Handler: func(*Server, map[string]any) (string, error) { return "ok-result", nil }})
	s.Register(Tool{Name: "probe_err", Handler: func(*Server, map[string]any) (string, error) { return "", errors.New("boom") }})
}

func TestToolCallPrefixesStaleWarning(t *testing.T) {
	s, _ := newTestServer(t)
	registerProbeTools(s)
	s.SetBuildInfo("aaaaaaaaaaaa", t.TempDir())
	h := &fakeHash{value: "bbbbbbbbbbbb"}
	s.stale.hash = h.fn
	s.stale.now = newClock().now
	want := (&staleChecker{buildHash: "aaaaaaaaaaaa", srcDir: "/src", hash: h.fn, now: newClock().now}).warning()
	if want == "" {
		t.Fatal("precondition: stale checker gives no warning")
	}

	ok := callTool(t, s, "probe_ok")
	if ok.IsError || !strings.HasPrefix(ok.Content[0].Text, want) || !strings.Contains(ok.Content[0].Text, "ok-result") {
		t.Fatalf("success result: isError=%v text=%q", ok.IsError, ok.Content[0].Text)
	}
	bad := callTool(t, s, "probe_err")
	if !bad.IsError || !strings.HasPrefix(bad.Content[0].Text, want) || !strings.Contains(bad.Content[0].Text, "boom") {
		t.Fatalf("error result: isError=%v text=%q", bad.IsError, bad.Content[0].Text)
	}
}

func TestToolCallNoWarningWithoutBuildInfo(t *testing.T) {
	s, _ := newTestServer(t)
	registerProbeTools(s)
	if got := callTool(t, s, "probe_ok").Content[0].Text; got != "ok-result" {
		t.Fatalf("success text = %q", got)
	}
	if got := callTool(t, s, "probe_err").Content[0].Text; got != "boom" {
		t.Fatalf("error text = %q", got)
	}
}

func TestToolCallNoWarningWhenFresh(t *testing.T) {
	s, _ := newTestServer(t)
	registerProbeTools(s)
	s.SetBuildInfo("aaaaaaaaaaaa", t.TempDir())
	h := &fakeHash{value: "aaaaaaaaaaaa"}
	s.stale.hash = h.fn
	s.stale.now = newClock().now
	if got := callTool(t, s, "probe_ok").Content[0].Text; got != "ok-result" {
		t.Fatalf("fresh build text = %q", got)
	}
}
