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

	"github.com/zengxs/yaqt/internal/httpclient"
)

const maxRepositoryResourceSize = 4 << 20

type repositoryResource struct {
	description string
	accept      string
}

var (
	repositoryIndexResource = repositoryResource{
		description: "Qt repository index",
		accept:      "text/html",
	}
	packageMetadataResource = repositoryResource{
		description: "Qt package metadata",
		accept:      "application/xml, text/xml",
	}
)

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
	index, err := c.fetchResource(ctx, repository.IndexURL(), repositoryIndexResource)
	if err != nil {
		return nil, err
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

func (c *Client) fetchResource(
	ctx context.Context,
	resourceURL string,
	resource repositoryResource,
) ([]byte, error) {
	response, err := httpclient.Get(ctx, c.httpClient, httpclient.Resource{
		URL:         resourceURL,
		Accept:      resource.accept,
		Description: resource.description,
	})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	contents, err := io.ReadAll(io.LimitReader(response.Body, maxRepositoryResourceSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s %s: %w", resource.description, resourceURL, err)
	}
	if len(contents) > maxRepositoryResourceSize {
		return nil, fmt.Errorf(
			"%s %s exceeds %d bytes",
			resource.description,
			resourceURL,
			maxRepositoryResourceSize,
		)
	}
	return contents, nil
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
	if repository.Target != TargetQt {
		return false
	}
	switch entry.Extension {
	case "unix_line_endings_src", "windows_line_endings_src", "src_doc_examples":
		return true
	default:
		return false
	}
}
