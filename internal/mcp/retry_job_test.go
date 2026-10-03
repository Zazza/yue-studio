package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// retryFake — fakeService + RetryJob, записывающий id и отдающий err.
type retryFake struct {
	*fakeService
	retried []int64
	err     error
}

func (f *retryFake) RetryJob(ctx context.Context, jobID int64) error {
	f.retried = append(f.retried, jobID)
	return f.err
}

func newRetryServer(t *testing.T) (*Server, *retryFake) {
	t.Helper()
	fake := &retryFake{fakeService: &fakeService{url: "http://w:8091"}}
	s := NewServer(fake, t.TempDir())
	RegisterWorkflowTools(s)
	RegisterStudioTools(s)
	RegisterLibraryTools(s)
	RegisterInstallTools(s)
	return s, fake
}

func TestRetryJobCallsService(t *testing.T) {
	s, fake := newRetryServer(t)
	out, ok := call(t, s, "retry_job", map[string]any{"job_id": float64(321)})
	if !ok {
		t.Fatalf("retry_job failed: %s", out)
	}
	if len(fake.retried) != 1 || fake.retried[0] != 321 {
		t.Fatalf("RetryJob calls %v, want [321]", fake.retried)
	}
	if !strings.Contains(out, "321") {
		t.Fatalf("out must mention job id: %s", out)
	}
}

func TestRetryJobBadIDIsError(t *testing.T) {
	for _, args := range []map[string]any{
		{},
		{"job_id": float64(0)},
		{"job_id": float64(-5)},
	} {
		s, fake := newRetryServer(t)
		out, ok := call(t, s, "retry_job", args)
		if ok {
			t.Fatalf("%v: expected tool error, got %s", args, out)
		}
		if len(fake.retried) != 0 {
			t.Fatalf("%v: service must not be called: %v", args, fake.retried)
		}
	}
}

func TestRetryJobServiceErrorIsError(t *testing.T) {
	s, fake := newRetryServer(t)
	fake.err = errors.New("409: задача не в ошибке")
	out, ok := call(t, s, "retry_job", map[string]any{"job_id": float64(8)})
	if ok {
		t.Fatalf("expected tool error, got %s", out)
	}
	if len(fake.retried) != 1 || fake.retried[0] != 8 {
		t.Fatalf("RetryJob calls %v", fake.retried)
	}
}
