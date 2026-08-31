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
	sampleFilesArchiveBase64           = "N3q8ryccAASgR6WICAAAAAAAAABmAAAAAAAAAN2R8/FiYXIKZm9vCgEEBgACCQQEAAcLAgABAQABAQAMBAQACAoB6bOiBKhlMn4AAAUCGQUAAAAAABERAGIAYQByAAAAZgBvAG8AAAAZAgAAFBIBAACFM3PyY9YBAFgCcvJj1gEVCgEAIICkgSCApIEAAA=="
	sampleEmptyArchiveBase64           = "N3q8ryccAARRXX0LTwAAAAAAAAAhAAAAAAAAACYYE6IAAIF8DAZ+A1ZFVvN9ynHKnCpDiSWTJixRow5LG1M9D6gA2ophUTr/mAu1kHLyGofL67CjcGya+qOxKewF/wR6EuubYHGGuey2DXtgAAAAFwYAAQlPAAcLAQABIwMBAQVdABAAAAyAzgoBLNuLJwAA"
	sampleFrameworkArchiveBase64       = "N3q8ryccAANPI/j4KgEAAAAAAAAiAAAAAAAAADpKRdUAKxlKR1xkugTyrt4TmoFPkOz1maKegBcr9L71wvEOGRnKCO1cKgMf//E/TwAAAIEzB64P0NNtfJ8/R0FY1v4CaiWo6ICFe2/DTFRjyZ5giHy4cphRxlqjz+ExkLQhggWFf9r7TjmBSckPob+gudXGRef79+VRKXzxE5u5GTExMWgRzzYNB4n6Y6QoV084Q4/rXZy0+IvkwmoPpXk/8qSkdSkm4eDkmj774IvAIXL2rTnVhpbQQcmhEHriq5AYM5ssw1W/u9v7RyUqeHZaiqEZy7XyZLc1VGP96BdE7Ls4+PvU+Y65clb0YlzSukjSktPTS6sx556hGwjIeakzRi0ivUrV66k9C1eWpFt22z28am09akoWeX4yCHsTa+l/hJwcXspK//+oKGdNFwYtAQmA/QAHCwEAASMDAQEFXQAAgAAMg3UKAWHrkLEAAA=="
	sampleEscapingSymlinkArchiveBase64 = "N3q8ryccAAP7RiuPoQAAAAAAAAAhAAAAAAAAACkwOQoAF2C43nU5f1WWx+XDGLG6LyR+jFsQbdbBSYrkyh6nK7Q6b5v//2hQAAAAAIEzB64P0KYGvJ8/R0FY1v4CaiWo6ICFeqHsdnxuhIiTrlNUDdJF0j4hjA39Dr7tpxc6oVVQHdle9cfvgnyx5VIIJwu2P8/gQbXI1qFowNcWUYxgqhxsHpP5vKEEaU3LdXdelB2/8bT71ZEKjy2b+F3/9bB8ABcGKgEJdwAHCwEAASMDAQEFXQAAgAAMgI4KAXFwy4oAAA=="
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

func TestSevenZipExtractorExtractsFrameworkSymbolicLinks(t *testing.T) {
	destination := newTestCacheDir(t)
	archive := writeTestArchive(t, sampleFrameworkArchiveBase64)

	err := (SevenZipExtractor{}).Extract(
		context.Background(),
		archive,
		destination,
	)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if err := (SevenZipExtractor{}).Extract(context.Background(), archive, destination); err != nil {
		t.Fatalf("Extract() second call error = %v", err)
	}

	framework := filepath.Join(destination, "lib", "QtConcurrent.framework")
	for path, want := range map[string]string{
		filepath.Join(framework, "Resources"):           filepath.Join("Versions", "Current", "Resources"),
		filepath.Join(framework, "Versions", "Current"): "A",
	} {
		got, err := os.Readlink(path)
		if err != nil {
			t.Errorf("read extracted symbolic link %q: %v", path, err)
			continue
		}
		if got != want {
			t.Errorf("extracted symbolic link %q = %q, want %q", path, got, want)
		}
	}

	contents, err := os.ReadFile(filepath.Join(framework, "Resources", "Info.plist"))
	if err != nil {
		t.Fatalf("read file through framework symbolic links: %v", err)
	}
	if got, want := string(contents), "fixture\n"; got != want {
		t.Errorf("framework Info.plist = %q, want %q", got, want)
	}
}

func TestSevenZipExtractorRejectsEscapingSymlinkBeforeWriting(t *testing.T) {
	destination := filepath.Join(newTestCacheDir(t), "destination")

	err := (SevenZipExtractor{}).Extract(
		context.Background(),
		writeTestArchive(t, sampleEscapingSymlinkArchiveBase64),
		destination,
	)
	if err == nil || !strings.Contains(err.Error(), "escapes the extraction root") {
		t.Fatalf("Extract() error = %v, want an escaping symbolic link error", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Errorf("destination exists after failed preflight; stat error = %v", err)
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
		{name: "symbolic link", entryName: "link", mode: fs.ModeSymlink | 0o777, want: "link"},
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

func TestValidateSymlinkTarget(t *testing.T) {
	tests := []struct {
		name      string
		entryName string
		target    string
		want      string
	}{
		{
			name:      "framework resource",
			entryName: "lib/QtConcurrent.framework/Resources",
			target:    "Versions/Current/Resources",
			want:      filepath.Join("Versions", "Current", "Resources"),
		},
		{
			name:      "parent traversal stays inside root",
			entryName: "lib/framework/link",
			target:    "../../shared",
			want:      filepath.Join("..", "..", "shared"),
		},
		{name: "parent traversal escapes root", entryName: "link", target: "../outside"},
		{name: "nested traversal escapes root", entryName: "lib/link", target: "../../outside"},
		{name: "absolute target", entryName: "link", target: "/outside"},
		{name: "backslash target", entryName: "link", target: `..\outside`},
		{name: "drive target", entryName: "link", target: "C:/outside"},
		{name: "empty target", entryName: "link"},
		{name: "nul target", entryName: "link", target: "inside\x00outside"},
		{name: "invalid UTF-8 target", entryName: "link", target: string([]byte{0xff})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateSymlinkTarget(test.entryName, test.target)
			if test.want == "" {
				if err == nil {
					t.Fatalf("validateSymlinkTarget(%q, %q) = %q, nil; want an error", test.entryName, test.target, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateSymlinkTarget(%q, %q) error = %v", test.entryName, test.target, err)
			}
			if got != test.want {
				t.Errorf("validateSymlinkTarget(%q, %q) = %q, want %q", test.entryName, test.target, got, test.want)
			}
		})
	}
}

func TestValidateArchiveLayoutRejectsTransitiveSymlinkEscape(t *testing.T) {
	entries := []extractionEntry{
		{
			relativePath: filepath.FromSlash("dir/inner"),
			mode:         fs.ModeSymlink | 0o777,
			linkTarget:   filepath.FromSlash("../safe"),
		},
		{
			relativePath: filepath.FromSlash("dir/link"),
			mode:         fs.ModeSymlink | 0o777,
			linkTarget:   filepath.FromSlash("inner/../../outside"),
		},
	}

	err := validateArchiveLayout(entries)
	if err == nil || !strings.Contains(err.Error(), "after symbolic-link resolution") {
		t.Fatalf("validateArchiveLayout() error = %v, want a transitive escape error", err)
	}
}

func TestValidateArchiveLayoutAllowsSafeTransitiveParentTraversal(t *testing.T) {
	entries := []extractionEntry{
		{
			relativePath: filepath.FromSlash("dir/inner"),
			mode:         fs.ModeSymlink | 0o777,
			linkTarget:   filepath.FromSlash("../safe"),
		},
		{
			relativePath: filepath.FromSlash("dir/link"),
			mode:         fs.ModeSymlink | 0o777,
			linkTarget:   filepath.FromSlash("inner/../inside"),
		},
	}

	if err := validateArchiveLayout(entries); err != nil {
		t.Fatalf("validateArchiveLayout() error = %v", err)
	}
}

func TestValidateArchiveLayoutRejectsSymlinkCycle(t *testing.T) {
	entries := []extractionEntry{
		{relativePath: "first", mode: fs.ModeSymlink | 0o777, linkTarget: "second"},
		{relativePath: "second", mode: fs.ModeSymlink | 0o777, linkTarget: "first"},
	}

	err := validateArchiveLayout(entries)
	if err == nil || !strings.Contains(err.Error(), "symbolic link cycle") {
		t.Fatalf("validateArchiveLayout() error = %v, want a symbolic link cycle error", err)
	}
}

func TestValidateSymlinkTargetsRejectsEscapeThroughExistingLink(t *testing.T) {
	destination := newTestCacheDir(t)
	if err := os.MkdirAll(filepath.Join(destination, "safe"), 0o755); err != nil {
		t.Fatalf("create safe directory: %v", err)
	}
	if err := os.Symlink(
		filepath.FromSlash("../../outside"),
		filepath.Join(destination, "safe", "redirect"),
	); err != nil {
		t.Skipf("create symbolic link: %v", err)
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatalf("open destination root: %v", err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Errorf("close destination root: %v", err)
		}
	}()

	entries := []extractionEntry{{
		relativePath: "link",
		mode:         fs.ModeSymlink | 0o777,
		linkTarget:   filepath.FromSlash("safe/redirect/file"),
	}}
	err = validateSymlinkTargets(root, entries)
	if err == nil || !strings.Contains(err.Error(), "after symbolic-link resolution") {
		t.Fatalf("validateSymlinkTargets() error = %v, want an existing-link escape error", err)
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

func TestValidateArchiveLayoutRejectsSymlinkUsedAsParentDirectory(t *testing.T) {
	entries := []extractionEntry{
		{relativePath: "lib", mode: fs.ModeSymlink | 0o777},
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
