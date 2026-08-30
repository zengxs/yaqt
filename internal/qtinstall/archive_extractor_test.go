package qtinstall

import (
	"context"
	"encoding/base64"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	sampleFilesArchiveBase64 = "N3q8ryccAASgR6WICAAAAAAAAABmAAAAAAAAAN2R8/FiYXIKZm9vCgEEBgACCQQEAAcLAgABAQABAQAMBAQACAoB6bOiBKhlMn4AAAUCGQUAAAAAABERAGIAYQByAAAAZgBvAG8AAAAZAgAAFBIBAACFM3PyY9YBAFgCcvJj1gEVCgEAIICkgSCApIEAAA=="
	sampleEmptyArchiveBase64 = "N3q8ryccAARRXX0LTwAAAAAAAAAhAAAAAAAAACYYE6IAAIF8DAZ+A1ZFVvN9ynHKnCpDiSWTJixRow5LG1M9D6gA2ophUTr/mAu1kHLyGofL67CjcGya+qOxKewF/wR6EuubYHGGuey2DXtgAAAAFwYAAQlPAAcLAQABIwMBAQVdABAAAAyAzgoBLNuLJwAA"
)

func TestSevenZipExtractorExtractsFilesAndDirectories(t *testing.T) {
	extractor := SevenZipExtractor{}

	filesDestination := filepath.Join(newTestCacheDir(t), "files")
	filesArchive := writeTestArchive(t, sampleFilesArchiveBase64)
	if err := extractor.Extract(context.Background(), filesArchive, filesDestination); err != nil {
		t.Fatalf("Extract(files) error = %v", err)
	}
	for name, want := range map[string]string{"bar": "bar\n", "foo": "foo\n"} {
		contents, err := os.ReadFile(filepath.Join(filesDestination, name))
		if err != nil {
			t.Fatalf("read extracted file %q: %v", name, err)
		}
		if got := string(contents); got != want {
			t.Errorf("extracted file %q = %q, want %q", name, got, want)
		}
	}

	emptyDestination := filepath.Join(newTestCacheDir(t), "empty")
	emptyArchive := writeTestArchive(t, sampleEmptyArchiveBase64)
	if err := extractor.Extract(context.Background(), emptyArchive, emptyDestination); err != nil {
		t.Fatalf("Extract(empty) error = %v", err)
	}
	info, err := os.Stat(filepath.Join(emptyDestination, "01"))
	if err != nil {
		t.Fatalf("stat extracted directory: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("extracted entry 01 mode = %v, want a directory", info.Mode())
	}
	info, err = os.Stat(filepath.Join(emptyDestination, "06"))
	if err != nil {
		t.Fatalf("stat extracted empty file: %v", err)
	}
	if info.Size() != 0 || !info.Mode().IsRegular() {
		t.Errorf("extracted entry 06 = mode %v, size %d; want an empty regular file", info.Mode(), info.Size())
	}
}

func TestSevenZipExtractorReplacesExistingRegularFile(t *testing.T) {
	destination := newTestCacheDir(t)
	if err := os.WriteFile(filepath.Join(destination, "foo"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	err := (SevenZipExtractor{}).Extract(
		context.Background(),
		writeTestArchive(t, sampleFilesArchiveBase64),
		destination,
	)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(destination, "foo"))
	if err != nil {
		t.Fatalf("read replaced file: %v", err)
	}
	if got, want := string(contents), "foo\n"; got != want {
		t.Errorf("replaced file = %q, want %q", got, want)
	}
}

func TestSevenZipExtractorRejectsExistingSymbolicLinkBeforeWriting(t *testing.T) {
	destination := newTestCacheDir(t)
	outside := filepath.Join(newTestCacheDir(t), "outside")
	if err := os.WriteFile(outside, []byte("preserve"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(destination, "foo")); err != nil {
		t.Skipf("create symbolic link: %v", err)
	}

	err := (SevenZipExtractor{}).Extract(
		context.Background(),
		writeTestArchive(t, sampleFilesArchiveBase64),
		destination,
	)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Extract() error = %v, want a symbolic link error", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "bar")); !os.IsNotExist(err) {
		t.Errorf("bar was written before preflight completed; stat error = %v", err)
	}
	contents, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read outside file: %v", err)
	}
	if got, want := string(contents), "preserve"; got != want {
		t.Errorf("outside file = %q, want %q", got, want)
	}
}

func TestSevenZipExtractorHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	destination := filepath.Join(newTestCacheDir(t), "destination")

	err := (SevenZipExtractor{}).Extract(ctx, writeTestArchive(t, sampleFilesArchiveBase64), destination)
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("Extract() error = %v, want context cancellation", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Errorf("destination exists after canceled extraction; stat error = %v", err)
	}
}

func TestValidateArchiveEntry(t *testing.T) {
	tests := []struct {
		name      string
		entryName string
		mode      fs.FileMode
		want      string
	}{
		{name: "nested file", entryName: "6.8.0/android_arm64_v8a/lib/libQt6Core.so", mode: 0o644, want: filepath.Join("6.8.0", "android_arm64_v8a", "lib", "libQt6Core.so")},
		{name: "parent traversal", entryName: "../outside", mode: 0o644},
		{name: "embedded traversal", entryName: "lib/../../outside", mode: 0o644},
		{name: "absolute path", entryName: "/outside", mode: 0o644},
		{name: "backslash path", entryName: `..\outside`, mode: 0o644},
		{name: "drive path", entryName: "C:/outside", mode: 0o644},
		{name: "empty path", entryName: "", mode: 0o644},
		{name: "symbolic link", entryName: "link", mode: fs.ModeSymlink | 0o777},
		{name: "device", entryName: "device", mode: fs.ModeDevice | 0o600},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateArchiveEntry(test.entryName, test.mode)
			if test.want == "" {
				if err == nil {
					t.Fatalf("validateArchiveEntry(%q) = %q, nil; want an error", test.entryName, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateArchiveEntry(%q) error = %v", test.entryName, err)
			}
			if got != test.want {
				t.Errorf("validateArchiveEntry(%q) = %q, want %q", test.entryName, got, test.want)
			}
		})
	}
}

func TestValidateArchiveLayoutRejectsFileUsedAsParentDirectory(t *testing.T) {
	entries := []extractionEntry{
		{relativePath: "lib", mode: 0o644},
		{relativePath: filepath.Join("lib", "libQt6Core.so"), mode: 0o644},
	}

	err := validateArchiveLayout(entries)
	if err == nil || !strings.Contains(err.Error(), "non-directory parent") {
		t.Fatalf("validateArchiveLayout() error = %v, want a non-directory parent error", err)
	}
}

func TestArchivePermissionsPreserveExecutableBitsWithoutBroadWriteAccess(t *testing.T) {
	tests := []struct {
		name string
		mode fs.FileMode
		want fs.FileMode
	}{
		{name: "executable file", mode: 0o777, want: 0o755},
		{name: "regular file", mode: 0o666, want: 0o644},
		{name: "private file", mode: 0o600, want: 0o600},
		{name: "default file", want: 0o644},
		{name: "default directory", mode: fs.ModeDir, want: 0o755},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := archivePermissions(test.mode); got != test.want {
				t.Errorf("archivePermissions(%v) = %v, want %v", test.mode, got, test.want)
			}
		})
	}
}

func writeTestArchive(t *testing.T, encoded string) string {
	t.Helper()
	contents, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode test archive: %v", err)
	}
	path := filepath.Join(newTestCacheDir(t), "sample.7z")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write test archive: %v", err)
	}
	return path
}
