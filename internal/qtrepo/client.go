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

	"github.com/zengxs/yaqt/internal/cache"
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
	httpClient    *http.Client
	resourceCache *repositoryResourceCache
}

// NewClient creates a repository client with the supplied HTTP client.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient}
}

// NewCachedClient creates a repository client that persists successful
// repository responses in the shared yaqt cache for fifteen minutes.
func NewCachedClient(httpClient *http.Client) *Client {
	client := NewClient(httpClient)
	client.resourceCache = newRepositoryResourceCache(defaultRepositoryCacheMaxAge)
	return client
}

// ListVersions returns supported stable Qt releases advertised by a repository index.
func (c *Client) ListVersions(ctx context.Context, repository Repository) ([]Version, error) {
	var versions []Version
	err := c.readResource(
		ctx,
		repository.IndexURL(),
		repositoryIndexResource,
		func(index []byte) error {
			var err error
			versions, err = parseVersions(bytes.NewReader(index), repository)
			if err != nil {
				return fmt.Errorf(
					"parse Qt repository index %s: %w",
					repository.IndexURL(),
					err,
				)
			}
			if len(versions) == 0 {
				return fmt.Errorf(
					"Qt repository index %s contains no supported stable versions (minimum %s)",
					repository.IndexURL(),
					minimumSupportedVersion,
				)
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return versions, nil
}

func (c *Client) readResource(
	ctx context.Context,
	resourceURL string,
	resource repositoryResource,
	parse func([]byte) error,
) error {
	if parse == nil {
		return fmt.Errorf("repository resource parser must not be nil")
	}
	load := func(ctx context.Context) ([]byte, error) {
		return c.fetchRemoteResource(ctx, resourceURL, resource)
	}
	if c.resourceCache == nil {
		contents, err := load(ctx)
		if err != nil {
			return err
		}
		return parse(contents)
	}
	cacheRoot, ok := cache.RootFromContext(ctx)
	if !ok {
		var err error
		cacheRoot, err = cache.ResolveRoot("")
		if err != nil {
			return err
		}
	}
	_, err := c.resourceCache.load(ctx, cacheRoot, resourceURL, load, parse)
	return err
}

func (c *Client) fetchRemoteResource(
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
	names, err := parseRepositoryChildNames(reader)
	if err != nil {
		return nil, err
	}

	versions := make(map[Version]struct{})
	for _, name := range names {
		entry, ok := parseRepositoryEntry(name)
		if ok && includeEntry(repository, entry) {
			versions[entry.Version] = struct{}{}
		}
	}
	result := make([]Version, 0, len(versions))
	for version := range versions {
		result = append(result, version)
	}
	slices.SortFunc(result, func(left, right Version) int {
		return left.compare(right)
	})
	return result, nil
}

func parseRepositoryChildNames(reader io.Reader) ([]string, error) {
	names := make(map[string]struct{})
	tokenizer := html.NewTokenizer(reader)

	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			if err := tokenizer.Err(); err != io.EOF {
				return nil, err
			}
			result := make([]string, 0, len(names))
			for name := range names {
				result = append(result, name)
			}
			slices.Sort(result)
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
				names[name] = struct{}{}
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
	return isSourceContentRepositoryExtension(entry.Extension)
}

func isSourceContentRepositoryExtension(extension string) bool {
	switch extension {
	case "unix_line_endings_src", "windows_line_endings_src", "src_doc_examples":
		return true
	default:
		return false
	}
}
