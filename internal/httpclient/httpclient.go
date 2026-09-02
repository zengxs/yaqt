// Package httpclient contains shared HTTP request policy for yaqt clients.
package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/zengxs/yaqt/internal/buildinfo"
)

const maxGetAttempts = 3

var getRetryDelays = [...]time.Duration{
	250 * time.Millisecond,
	1 * time.Second,
}

// Resource describes one HTTP resource to retrieve.
type Resource struct {
	URL         string
	Accept      string
	Description string
}

type statusError struct {
	resource Resource
	status   string
	code     int
}

func (err *statusError) Error() string {
	return fmt.Sprintf(
		"fetch %s %s: server returned %s",
		err.resource.Description,
		err.resource.URL,
		err.status,
	)
}

// HasStatusCode reports whether err came from an HTTP response with code.
func HasStatusCode(err error, code int) bool {
	var responseError *statusError
	return errors.As(err, &responseError) && responseError.code == code
}

// Get retrieves resource after applying yaqt request headers, retry, and status
// policy. The caller owns the response body when Get succeeds.
func Get(ctx context.Context, client *http.Client, resource Resource) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}

	for attempt := range maxGetAttempts {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, resource.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("create %s request: %w", resource.Description, err)
		}
		request.Header.Set("Accept", resource.Accept)
		request.Header.Set("User-Agent", buildinfo.UserAgent)

		response, err := client.Do(request)
		if err == nil {
			if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
				_ = response.Body.Close()
				return nil, &statusError{
					resource: resource,
					status:   response.Status,
					code:     response.StatusCode,
				}
			}
			return response, nil
		}
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, fmt.Errorf(
				"fetch %s %s: %w",
				resource.Description,
				resource.URL,
				contextErr,
			)
		}
		if attempt == maxGetAttempts-1 {
			return nil, fmt.Errorf(
				"fetch %s %s after %d attempts: %w",
				resource.Description,
				resource.URL,
				maxGetAttempts,
				err,
			)
		}
		if err := waitForRetry(ctx, getRetryDelays[attempt]); err != nil {
			return nil, fmt.Errorf(
				"fetch %s %s: %w",
				resource.Description,
				resource.URL,
				err,
			)
		}
	}
	panic("unreachable")
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
