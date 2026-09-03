package qtinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/zengxs/yaqt/internal/filelock"
	"github.com/zengxs/yaqt/internal/httpclient"
	"github.com/zengxs/yaqt/internal/qtrepo"
)

const maxChecksumFileSize = 4 << 10

type checksumPolicy struct {
	algorithm     qtrepo.ChecksumAlgorithm
	displayName   string
	sidecarSuffix string
	digestSize    int
	newHasher     func() hash.Hash
}

// ArchiveStore downloads Qt archives into a content-addressed cache and only
// publishes files whose declared checksum has been verified.
type ArchiveStore struct {
	httpClient *http.Client
	cacheDir   string
}

// NewArchiveStore creates an archive store rooted at cacheDir.
func NewArchiveStore(httpClient *http.Client, cacheDir string) (*ArchiveStore, error) {
	if cacheDir == "" {
		return nil, fmt.Errorf("cache directory must not be empty")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &ArchiveStore{
		httpClient: httpClient,
		cacheDir:   filepath.Clean(cacheDir),
	}, nil
}

// Fetch returns a verified local path for archive. An existing cache entry is
// revalidated before reuse, and an invalid entry is replaced.
func (store *ArchiveStore) Fetch(ctx context.Context, archive qtrepo.Archive) (string, error) {
	if store == nil {
		return "", fmt.Errorf("archive store must not be nil")
	}
	policy, err := checksumPolicyFor(archive.Checksum.Algorithm)
	if err != nil {
		return "", err
	}
	if archive.URL == "" {
		return "", fmt.Errorf("archive URL must not be empty")
	}
	expected, err := store.fetchChecksum(ctx, archive.Checksum, policy)
	if err != nil {
		return "", err
	}

	downloadDir := filepath.Join(store.cacheDir, "downloads", string(policy.algorithm))
	finalPath := filepath.Join(downloadDir, expected+".7z")
	cacheRoot, _, err := openManagedPathRoot(store.cacheDir, true)
	if err != nil {
		return "", fmt.Errorf("open archive cache root %s: %w", store.cacheDir, err)
	}
	defer func() { _ = cacheRoot.close() }()
	if err := cacheRoot.ensureDirectory(downloadDir, "archive cache directory"); err != nil {
		return "", fmt.Errorf("create archive cache directory %s: %w", downloadDir, err)
	}
	lockDir := filepath.Join(store.cacheDir, "locks", string(policy.algorithm))
	if err := cacheRoot.ensureDirectory(lockDir, "archive cache lock directory"); err != nil {
		return "", fmt.Errorf("create archive cache lock directory %s: %w", lockDir, err)
	}
	lockPath := filepath.Join(lockDir, expected+".lock")
	relativeLockPath, err := cacheRoot.relativePath(lockPath)
	if err != nil {
		return "", fmt.Errorf("resolve archive cache lock %s: %w", lockPath, err)
	}
	lockFile, err := filelock.OpenFile(cacheRoot.root, relativeLockPath, 0o644)
	if err != nil {
		return "", fmt.Errorf("open archive cache lock %s: %w", lockPath, err)
	}
	return withArchiveCacheLock(
		ctx,
		lockFile,
		lockPath,
		func() (string, error) {
			if _, err := cacheRoot.inspectFile(finalPath, "cached archive"); err != nil {
				return "", err
			}
			valid, err := archiveMatchesChecksum(finalPath, expected, policy)
			if err != nil {
				return "", fmt.Errorf("verify cached archive %s: %w", finalPath, err)
			}
			if valid {
				return finalPath, nil
			}
			if err := os.Remove(finalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("remove invalid cached archive %s: %w", finalPath, err)
			}
			return store.downloadArchive(ctx, archive.URL, expected, downloadDir, finalPath, policy)
		},
	)
}

func withArchiveCacheLock(
	ctx context.Context,
	file *os.File,
	lockPath string,
	operation func() (string, error),
) (result string, resultErr error) {
	lock, err := filelock.Acquire(ctx, file, nil)
	if err != nil {
		return "", fmt.Errorf("lock archive cache entry %s: %w", lockPath, err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("release archive cache lock: %w", err))
		}
	}()
	return operation()
}

func checksumPolicyFor(algorithm qtrepo.ChecksumAlgorithm) (checksumPolicy, error) {
	switch algorithm {
	case qtrepo.ChecksumSHA256:
		return checksumPolicy{
			algorithm:     qtrepo.ChecksumSHA256,
			displayName:   "SHA-256",
			sidecarSuffix: ".sha256",
			digestSize:    sha256.Size,
			newHasher:     sha256.New,
		}, nil
	default:
		return checksumPolicy{}, fmt.Errorf("unsupported checksum algorithm %q", algorithm)
	}
}

func (store *ArchiveStore) fetchChecksum(
	ctx context.Context,
	checksum qtrepo.Checksum,
	policy checksumPolicy,
) (string, error) {
	parsed, err := url.Parse(checksum.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("invalid %s checksum URL %q", policy.displayName, checksum.URL)
	}
	if !strings.HasSuffix(strings.ToLower(parsed.Path), policy.sidecarSuffix) {
		return "", fmt.Errorf(
			"%s checksum URL %q must end in %s",
			policy.displayName,
			checksum.URL,
			policy.sidecarSuffix,
		)
	}

	response, err := httpclient.Get(ctx, store.httpClient, httpclient.Resource{
		URL:         checksum.URL,
		Accept:      "text/plain, application/octet-stream",
		Description: policy.displayName + " checksum",
	})
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	contents, err := io.ReadAll(io.LimitReader(response.Body, maxChecksumFileSize+1))
	if err != nil {
		return "", fmt.Errorf("read %s checksum %s: %w", policy.displayName, checksum.URL, err)
	}
	if len(contents) > maxChecksumFileSize {
		return "", fmt.Errorf(
			"%s checksum %s exceeds %d bytes",
			policy.displayName,
			checksum.URL,
			maxChecksumFileSize,
		)
	}
	digest, err := parseChecksum(contents, policy)
	if err != nil {
		return "", fmt.Errorf("read %s checksum %s: %w", policy.displayName, checksum.URL, err)
	}
	return digest, nil
}

func (store *ArchiveStore) downloadArchive(
	ctx context.Context,
	archiveURL string,
	expected string,
	downloadDir string,
	finalPath string,
	policy checksumPolicy,
) (string, error) {
	response, err := httpclient.Get(ctx, store.httpClient, httpclient.Resource{
		URL:         archiveURL,
		Accept:      "application/x-7z-compressed, application/octet-stream",
		Description: "Qt archive",
	})
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	partial, err := os.CreateTemp(downloadDir, expected+".7z.*.part")
	if err != nil {
		return "", fmt.Errorf("create partial archive in %s: %w", downloadDir, err)
	}
	partialPath := partial.Name()
	closed := false
	defer func() {
		if !closed {
			_ = partial.Close()
		}
		_ = os.Remove(partialPath)
	}()

	hasher := policy.newHasher()
	written, copyErr := io.Copy(io.MultiWriter(partial, hasher), response.Body)
	if copyErr != nil {
		return "", fmt.Errorf("download Qt archive %s: %w", archiveURL, copyErr)
	}
	if response.ContentLength >= 0 && written != response.ContentLength {
		return "", fmt.Errorf(
			"download Qt archive %s: received %d bytes, expected %d",
			archiveURL,
			written,
			response.ContentLength,
		)
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != expected {
		return "", fmt.Errorf(
			"download Qt archive %s: %s mismatch: expected %s, got %s",
			archiveURL,
			policy.displayName,
			expected,
			actual,
		)
	}
	if err := partial.Sync(); err != nil {
		return "", fmt.Errorf("sync partial archive %s: %w", partialPath, err)
	}
	closeErr := partial.Close()
	closed = true
	if closeErr != nil {
		return "", fmt.Errorf("close partial archive %s: %w", partialPath, closeErr)
	}
	if err := os.Rename(partialPath, finalPath); err != nil {
		valid, verifyErr := archiveMatchesChecksum(finalPath, expected, policy)
		if verifyErr == nil && valid {
			return finalPath, nil
		}
		return "", fmt.Errorf("publish verified archive %s: %w", finalPath, err)
	}
	return finalPath, nil
}

func parseChecksum(contents []byte, policy checksumPolicy) (string, error) {
	fields := bytes.Fields(contents)
	if len(fields) == 0 {
		return "", fmt.Errorf("invalid %s checksum: value is empty", policy.displayName)
	}
	digest := strings.ToLower(string(fields[0]))
	if len(digest) != policy.digestSize*2 {
		return "", fmt.Errorf("invalid %s checksum %q", policy.displayName, digest)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("invalid %s checksum %q", policy.displayName, digest)
	}
	return digest, nil
}

func archiveMatchesChecksum(path string, expected string, policy checksumPolicy) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	hasher := policy.newHasher()
	_, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil {
		return false, copyErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	return hex.EncodeToString(hasher.Sum(nil)) == expected, nil
}
