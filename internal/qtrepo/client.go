package qtrepo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

const maxIndexSize = 4 << 20

// Client reads metadata from Qt online repositories.
type Client struct {
	httpClient *http.Client
}

// NewClient creates a repository client with the supplied HTTP client.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient}
}

// ListVersions returns supported stable Qt releases advertised by a repository index.
func (c *Client) ListVersions(ctx context.Context, repository Repository) ([]Version, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, repository.IndexURL(), nil)
	if err != nil {
		return nil, fmt.Errorf("create repository index request: %w", err)
	}
	request.Header.Set("Accept", "text/html")
	request.Header.Set("User-Agent", "yaqt/0.1.0")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch Qt repository index %s: %w", repository.IndexURL(), err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf(
			"fetch Qt repository index %s: server returned %s",
			repository.IndexURL(),
			response.Status,
		)
	}

	index, err := io.ReadAll(io.LimitReader(response.Body, maxIndexSize+1))
	if err != nil {
		return nil, fmt.Errorf("read Qt repository index %s: %w", repository.IndexURL(), err)
	}
	if len(index) > maxIndexSize {
		return nil, fmt.Errorf("Qt repository index %s exceeds %d bytes", repository.IndexURL(), maxIndexSize)
	}

	versions, err := parseVersions(bytes.NewReader(index), repository)
	if err != nil {
		return nil, fmt.Errorf("parse Qt repository index %s: %w", repository.IndexURL(), err)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf(
			"Qt repository index %s contains no supported stable versions (minimum %s)",
			repository.IndexURL(),
			minimumSupportedVersion,
		)
	}
	return versions, nil
}

func parseVersions(reader io.Reader, repository Repository) ([]Version, error) {
	versions := make(map[Version]struct{})
	tokenizer := html.NewTokenizer(reader)

	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			if err := tokenizer.Err(); err != io.EOF {
				return nil, err
			}

			result := make([]Version, 0, len(versions))
			for version := range versions {
				result = append(result, version)
			}
			slices.SortFunc(result, func(left, right Version) int {
				return left.compare(right)
			})
			return result, nil

		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data != "a" {
				continue
			}

			for _, attribute := range token.Attr {
				if attribute.Key != "href" {
					continue
				}

				name, ok := directChildName(attribute.Val)
				if !ok {
					break
				}
				entry, ok := parseRepositoryEntry(name)
				if ok && includeEntry(repository, entry) {
					versions[entry.Version] = struct{}{}
				}
				break
			}
		}
	}
}

func directChildName(href string) (string, bool) {
	parsed, err := url.Parse(href)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}

	name := strings.TrimSuffix(parsed.Path, "/")
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

func includeEntry(repository Repository, entry repositoryEntry) bool {
	if entry.Version.compare(minimumSupportedVersion) < 0 {
		return false
	}
	if entry.Extension == "" {
		return true
	}
	if repository.Host != HostAllOS || repository.Target != TargetQt {
		return false
	}
	switch entry.Extension {
	case "unix_line_endings_src", "windows_line_endings_src", "src_doc_examples":
		return true
	default:
		return false
	}
}
