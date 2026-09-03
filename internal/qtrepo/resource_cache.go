package qtrepo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/zengxs/yaqt/internal/filelock"
)

const (
	defaultRepositoryCacheMaxAge = 15 * time.Minute
	repositoryCacheSchemaVersion = 1
	maxRepositoryCacheEntrySize  = 2*maxRepositoryResourceSize + 64<<10
	repositoryCacheTempAttempts  = 100
)

type repositoryCacheEntry struct {
	SchemaVersion int       `json:"schema_version"`
	ResourceKey   string    `json:"resource_key"`
	FetchedAt     time.Time `json:"fetched_at"`
	Checksum      string    `json:"sha256"`
	Contents      []byte    `json:"contents"`
}

type repositoryResourceCache struct {
	maxAge time.Duration
	now    func() time.Time
}

func newRepositoryResourceCache(maxAge time.Duration) *repositoryResourceCache {
	return &repositoryResourceCache{
		maxAge: maxAge,
		now:    time.Now,
	}
}

func (store *repositoryResourceCache) load(
	ctx context.Context,
	cacheRoot string,
	resourceURL string,
	load func(context.Context) ([]byte, error),
	validate func([]byte) error,
) (result []byte, resultErr error) {
	if store == nil {
		return nil, fmt.Errorf("repository resource cache must not be nil")
	}
	if store.maxAge <= 0 {
		return nil, fmt.Errorf("repository resource cache maximum age must be positive")
	}
	if load == nil {
		return nil, fmt.Errorf("repository resource loader must not be nil")
	}
	if validate == nil {
		return nil, fmt.Errorf("repository resource validator must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	root, err := openRepositoryCacheRoot(cacheRoot)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := root.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close repository cache root: %w", err))
		}
	}()

	entryPath, lockPath := repositoryCachePaths(resourceURL)
	if contents, found, err := store.readValidFresh(
		root,
		entryPath,
		resourceURL,
		validate,
	); err != nil {
		return nil, err
	} else if found {
		return contents, nil
	}

	lockFile, err := filelock.OpenFile(root, lockPath, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open repository cache lock %s: %w", lockPath, err)
	}
	lock, err := filelock.Acquire(ctx, lockFile, nil)
	if err != nil {
		return nil, fmt.Errorf("lock repository cache entry for %s: %w", resourceURL, err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("release repository cache lock: %w", err))
		}
	}()

	if contents, found, err := store.readValidFresh(
		root,
		entryPath,
		resourceURL,
		validate,
	); err != nil {
		return nil, err
	} else if found {
		return contents, nil
	}

	contents, err := load(ctx)
	if err != nil {
		return nil, err
	}
	if len(contents) > maxRepositoryResourceSize {
		return nil, fmt.Errorf(
			"repository resource %s exceeds %d bytes",
			resourceURL,
			maxRepositoryResourceSize,
		)
	}
	if err := validate(contents); err != nil {
		return nil, err
	}
	if err := store.write(root, entryPath, resourceURL, contents); err != nil {
		return nil, err
	}
	return contents, nil
}

func (store *repositoryResourceCache) readValidFresh(
	root *os.Root,
	entryPath string,
	resourceURL string,
	validate func([]byte) error,
) ([]byte, bool, error) {
	contents, found, err := store.readFresh(root, entryPath, resourceURL)
	if err != nil || !found {
		return contents, found, err
	}
	if err := validate(contents); err != nil {
		return nil, false, nil
	}
	return contents, true, nil
}

func openRepositoryCacheRoot(cacheRoot string) (*os.Root, error) {
	if cacheRoot == "" {
		return nil, fmt.Errorf("repository cache root must not be empty")
	}
	cleanRoot := filepath.Clean(cacheRoot)
	if err := os.MkdirAll(cleanRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create repository cache root %s: %w", cleanRoot, err)
	}
	root, err := os.OpenRoot(cleanRoot)
	if err != nil {
		return nil, fmt.Errorf("open repository cache root %s: %w", cleanRoot, err)
	}
	directories := []struct {
		path        string
		description string
	}{
		{repositoryCacheDataDirectory(), "repository metadata cache directory"},
		{repositoryCacheLockDirectory(), "repository metadata cache lock directory"},
	}
	for _, directory := range directories {
		if err := root.MkdirAll(directory.path, 0o755); err != nil {
			_ = root.Close()
			return nil, fmt.Errorf("create %s: %w", directory.description, err)
		}
	}
	return root, nil
}

func repositoryCachePaths(resourceURL string) (entryPath, lockPath string) {
	name := repositoryCacheKey(resourceURL)
	return filepath.Join(repositoryCacheDataDirectory(), name+".json"),
		filepath.Join(repositoryCacheLockDirectory(), name+".lock")
}

func repositoryCacheKey(resourceURL string) string {
	digest := sha256.Sum256([]byte(resourceURL))
	return hex.EncodeToString(digest[:])
}

func repositoryCacheDataDirectory() string {
	return filepath.Join("metadata", "sha256")
}

func repositoryCacheLockDirectory() string {
	return filepath.Join("locks", "metadata", "sha256")
}

func (store *repositoryResourceCache) readFresh(
	root *os.Root,
	entryPath string,
	resourceURL string,
) ([]byte, bool, error) {
	file, err := root.Open(entryPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open repository cache entry %s: %w", entryPath, err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("inspect repository cache entry %s: %w", entryPath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("repository cache entry %s is not a regular file", entryPath)
	}
	encoded, err := io.ReadAll(io.LimitReader(file, maxRepositoryCacheEntrySize+1))
	if err != nil {
		return nil, false, fmt.Errorf("read repository cache entry %s: %w", entryPath, err)
	}
	if len(encoded) > maxRepositoryCacheEntrySize {
		return nil, false, nil
	}

	var entry repositoryCacheEntry
	if err := json.Unmarshal(encoded, &entry); err != nil {
		return nil, false, nil
	}
	if entry.SchemaVersion != repositoryCacheSchemaVersion ||
		entry.ResourceKey != repositoryCacheKey(resourceURL) ||
		len(entry.Contents) > maxRepositoryResourceSize ||
		entry.FetchedAt.IsZero() {
		return nil, false, nil
	}
	age := store.now().UTC().Sub(entry.FetchedAt)
	if age < 0 || age > store.maxAge {
		return nil, false, nil
	}
	digest := sha256.Sum256(entry.Contents)
	if entry.Checksum != hex.EncodeToString(digest[:]) {
		return nil, false, nil
	}
	return entry.Contents, true, nil
}

func (store *repositoryResourceCache) write(
	root *os.Root,
	entryPath string,
	resourceURL string,
	contents []byte,
) error {
	digest := sha256.Sum256(contents)
	entry := repositoryCacheEntry{
		SchemaVersion: repositoryCacheSchemaVersion,
		ResourceKey:   repositoryCacheKey(resourceURL),
		FetchedAt:     store.now().UTC(),
		Checksum:      hex.EncodeToString(digest[:]),
		Contents:      contents,
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode repository cache entry for %s: %w", resourceURL, err)
	}
	if len(encoded) > maxRepositoryCacheEntrySize {
		return fmt.Errorf("repository cache entry for %s exceeds the cache size limit", resourceURL)
	}

	temporary, temporaryPath, err := createRepositoryCacheTemporaryFile(root, filepath.Base(entryPath))
	if err != nil {
		return fmt.Errorf("create repository cache entry for %s: %w", resourceURL, err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = root.Remove(temporaryPath)
	}()
	if _, err := temporary.Write(encoded); err != nil {
		return fmt.Errorf("write repository cache entry for %s: %w", resourceURL, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync repository cache entry for %s: %w", resourceURL, err)
	}
	if err := temporary.Close(); err != nil {
		closed = true
		return fmt.Errorf("close repository cache entry for %s: %w", resourceURL, err)
	}
	closed = true
	if err := root.Rename(temporaryPath, entryPath); err != nil {
		return fmt.Errorf("publish repository cache entry for %s: %w", resourceURL, err)
	}
	return nil
}

func createRepositoryCacheTemporaryFile(root *os.Root, entryName string) (*os.File, string, error) {
	for range repositoryCacheTempAttempts {
		var randomBytes [8]byte
		if _, err := rand.Read(randomBytes[:]); err != nil {
			return nil, "", err
		}
		name := "." + entryName + "-" + hex.EncodeToString(randomBytes[:]) + ".tmp"
		path := filepath.Join(repositoryCacheDataDirectory(), name)
		file, err := root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return file, path, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("could not create a unique repository cache temporary file")
}
