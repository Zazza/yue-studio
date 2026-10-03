package yue

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestRetryJobSendsPost(t *testing.T) {
	var got updateReq
	c := updateServer(t, 200, `{"retried":true}`, &got)
	if err := c.RetryJob(context.Background(), 42); err != nil {
		t.Fatalf("RetryJob: %v", err)
	}
	if got.method != http.MethodPost || got.path != "/jobs/42/retry" {
		t.Fatalf("request: %s %s", got.method, got.path)
	}
	if len(got.body) != 0 {
		t.Fatalf("retry needs no body fields: %v", got.body)
	}
}

func TestRetryJobErrorStatus(t *testing.T) {
	for _, tc := range []struct {
		code   int
		detail string
	}{
		{409, "задача не в ошибке"},
		{404, "job not found"},
	} {
		var got updateReq
		c := updateServer(t, tc.code, `{"detail":"`+tc.detail+`"}`, &got)
		err := c.RetryJob(context.Background(), 7)
		if err == nil {
			t.Fatalf("%d: expected error", tc.code)
		}
		var se *StatusError
		if !errors.As(err, &se) || se.Code != tc.code {
			t.Fatalf("%d: want *StatusError with code, got %T %v", tc.code, err, err)
		}
		if !strings.Contains(err.Error(), tc.detail) {
			t.Fatalf("%d: error must carry worker detail %q: %v", tc.code, tc.detail, err)
		}
		if got.path != "/jobs/7/retry" {
			t.Fatalf("%d: path %s", tc.code, got.path)
		}
	}
}
