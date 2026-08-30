// Package httpclient contains shared HTTP request policy for yaqt clients.
package httpclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/zengxs/yaqt/internal/buildinfo"
)

// Resource describes one HTTP resource to retrieve.
type Resource struct {
	URL         string
	Accept      string
	Description string
}

// Get retrieves resource after applying yaqt request headers and status policy.
// The caller owns the response body when Get succeeds.
func Get(ctx context.Context, client *http.Client, resource Resource) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, resource.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create %s request: %w", resource.Description, err)
	}
	request.Header.Set("Accept", resource.Accept)
	request.Header.Set("User-Agent", buildinfo.UserAgent)

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch %s %s: %w", resource.Description, resource.URL, err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_ = response.Body.Close()
		return nil, fmt.Errorf(
			"fetch %s %s: server returned %s",
			resource.Description,
			resource.URL,
			response.Status,
		)
	}
	return response, nil
}
