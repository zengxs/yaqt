package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/zengxs/yaqt/internal/buildinfo"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (body *trackedBody) Close() error {
	body.closed = true
	return nil
}

func TestGetAppliesRequestPolicyAndLeavesSuccessfulBodyOpen(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader("contents")}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got, want := request.Method, http.MethodGet; got != want {
			t.Errorf("request method = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("Accept"), "application/xml"; got != want {
			t.Errorf("Accept = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("User-Agent"), buildinfo.UserAgent; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       body,
		}, nil
	})}

	response, err := Get(context.Background(), client, Resource{
		URL:         "https://mirror.example/Updates.xml",
		Accept:      "application/xml",
		Description: "Qt package metadata",
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if body.closed {
		t.Fatal("Get() closed a successful response body")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("response.Body.Close() error = %v", err)
	}
}

func TestGetRetriesTransientRequestFailures(t *testing.T) {
	attempts := 0
	body := &trackedBody{Reader: strings.NewReader("contents")}
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		attempts++
		if attempts < 3 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       body,
		}, nil
	})}

	response, err := Get(context.Background(), client, Resource{
		URL:         "https://mirror.example/Updates.xml",
		Description: "Qt package metadata",
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()
	if got, want := attempts, 3; got != want {
		t.Fatalf("request attempts = %d, want %d", got, want)
	}
}

func TestGetDoesNotRetryCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		attempts++
		cancel()
		return nil, context.Canceled
	})}

	_, err := Get(ctx, client, Resource{
		URL:         "https://mirror.example/Updates.xml",
		Description: "Qt package metadata",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Get() error = %v, want context cancellation", err)
	}
	if got, want := attempts, 1; got != want {
		t.Fatalf("request attempts = %d, want %d", got, want)
	}
}

func TestGetClosesUnsuccessfulResponseBody(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader("unavailable")}
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
			Header:     make(http.Header),
			Body:       body,
		}, nil
	})}

	_, err := Get(context.Background(), client, Resource{
		URL:         "https://mirror.example/archive.7z",
		Description: "Qt archive",
	})
	if err == nil || !strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("Get() error = %v, want HTTP status", err)
	}
	if !body.closed {
		t.Fatal("Get() did not close an unsuccessful response body")
	}
}
