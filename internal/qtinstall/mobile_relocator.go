package qtinstall

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

const maximumRelocationFileSize = 1 << 20

var androidNDKHostSettingPattern = regexp.MustCompile(
	`(?m)^DEFAULT_ANDROID_NDK_HOST[ \t]*=[^\r\n]*`,
)

type mobileTargetProfile struct {
	displayName         string
	requiredHost        qtrepo.Host
	rewriteAndroidPaths bool
}

var mobileTargetProfiles = map[qtrepo.Target]mobileTargetProfile{
	qtrepo.TargetAndroid: {
		displayName:         "Android",
		rewriteAndroidPaths: true,
	},
	qtrepo.TargetIOS: {
		displayName:  "iOS",
		requiredHost: qtrepo.HostMac,
	},
}

// MobileRelocator connects an extracted mobile Qt kit to the desktop Qt tools
// that run on the selected host.
type MobileRelocator struct {
	target mobileTargetProfile
	hostQt hostQtInstallation
}

type relocationFileSpec struct {
	path      string
	transform func([]byte) ([]byte, error)
}

type relocationFileUpdate struct {
	path        string
	contents    []byte
	permissions fs.FileMode
	partialPath string
}

type iniReplacement struct {
	section string
	key     string
	value   string
	found   bool
}

// NewMobileRelocator finds and validates the matching desktop Qt beneath the
// version directory in qtRoot.
func NewMobileRelocator(
	target qtrepo.Target,
	requirement qtrepo.QtInstallationIdentity,
	qtRoot string,
) (*MobileRelocator, error) {
	targetProfile, ok := mobileTargetProfiles[target]
	if !ok {
		return nil, fmt.Errorf("target %q is not a mobile Qt target", target)
	}
	if targetProfile.requiredHost != "" && requirement.Host != targetProfile.requiredHost {
		return nil, fmt.Errorf(
			"%s Qt requires host %q, not %q",
			targetProfile.displayName,
			targetProfile.requiredHost,
			requirement.Host,
		)
	}
	hostQt, err := discoverHostQt(requirement, qtRoot)
	if err != nil {
		return nil, err
	}

	return &MobileRelocator{
		target: targetProfile,
		hostQt: hostQt,
	}, nil
}

// Validate checks every file required to relocate one mobile Qt kit without
// publishing any replacements.
func (relocator *MobileRelocator) Validate(ctx context.Context, kitDir string) error {
	return relocator.processKit(ctx, kitDir, false)
}

// Relocate rewrites one extracted mobile Qt kit. Every affected file is
// validated and staged before the first replacement is published.
func (relocator *MobileRelocator) Relocate(
	ctx context.Context,
	kitDir string,
) error {
	return relocator.processKit(ctx, kitDir, true)
}

func (relocator *MobileRelocator) processKit(
	ctx context.Context,
	kitDir string,
	apply bool,
) (resultErr error) {
	targetName := "mobile"
	if relocator != nil {
		targetName = relocator.target.displayName
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("relocate %s Qt kit: %w", targetName, err)
	}
	if relocator == nil {
		return fmt.Errorf("mobile Qt relocator is not configured")
	}
	if strings.TrimSpace(kitDir) == "" {
		return fmt.Errorf("%s Qt kit directory must not be empty", targetName)
	}
	if strings.ContainsAny(kitDir, "\r\n\x00") {
		return fmt.Errorf("%s Qt kit directory contains unsupported characters", targetName)
	}

	absoluteKitDir, err := filepath.Abs(kitDir)
	if err != nil {
		return fmt.Errorf("resolve %s Qt kit directory %s: %w", targetName, kitDir, err)
	}
	info, err := os.Stat(absoluteKitDir)
	if err != nil {
		return fmt.Errorf("inspect %s Qt kit directory %s: %w", targetName, absoluteKitDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s Qt kit path %s is not a directory", targetName, absoluteKitDir)
	}

	root, err := os.OpenRoot(absoluteKitDir)
	if err != nil {
		return fmt.Errorf("open %s Qt kit directory %s: %w", targetName, absoluteKitDir, err)
	}
	defer func() {
		if err := root.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close %s Qt kit directory %s: %w", targetName, absoluteKitDir, err)
		}
	}()

	updates := make([]relocationFileUpdate, 0, 6)
	for _, spec := range relocator.fileSpecs(absoluteKitDir) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("relocate %s Qt kit: %w", targetName, err)
		}
		update, changed, err := prepareRelocationFile(root, spec)
		if err != nil {
			return fmt.Errorf("prepare %s Qt relocation for %s: %w", targetName, spec.path, err)
		}
		if changed {
			updates = append(updates, update)
		}
	}
	if !apply {
		return nil
	}

	if err := applyRelocationFiles(ctx, root, updates); err != nil {
		return fmt.Errorf("apply %s Qt relocation: %w", targetName, err)
	}
	return nil
}

func (relocator *MobileRelocator) fileSpecs(kitDir string) []relocationFileSpec {
	profile := relocator.hostQt.profile
	specs := make([]relocationFileSpec, 0, 6)
	wrappers := []struct {
		name     string
		hostTool string
	}{
		{name: "qmake", hostTool: "qmake6"},
		{name: "qmake6", hostTool: "qmake6"},
		{name: "qtpaths", hostTool: "qtpaths6"},
		{name: "qtpaths6", hostTool: "qtpaths6"},
	}
	for _, wrapper := range wrappers {
		hostToolPath := relocator.hostQt.toolPath(wrapper.hostTool)
		specs = append(specs, relocationFileSpec{
			path: filepath.Join("bin", wrapper.name+profile.wrapperExtension),
			transform: func(contents []byte) ([]byte, error) {
				return rewriteQtToolScript(contents, wrapper.hostTool, hostToolPath, profile.windows)
			},
		})
	}
	specs = append(specs, relocationFileSpec{
		path: filepath.Join("bin", "target_qt.conf"),
		transform: func(contents []byte) ([]byte, error) {
			return rewriteTargetQtConfiguration(
				contents,
				kitDir,
				relocator.hostQt.directory,
				profile.hostLibraryExecutables,
			)
		},
	})
	if relocator.target.rewriteAndroidPaths {
		specs = append(specs, relocationFileSpec{
			path: filepath.Join("mkspecs", "qdevice.pri"),
			transform: func(contents []byte) ([]byte, error) {
				return rewriteAndroidNDKHost(contents, profile.androidNDKHost)
			},
		})
	}
	return specs
}

func prepareRelocationFile(
	root *os.Root,
	spec relocationFileSpec,
) (relocationFileUpdate, bool, error) {
	if err := validateExistingParents(root, spec.path); err != nil {
		return relocationFileUpdate{}, false, err
	}
	info, err := root.Lstat(spec.path)
	if err != nil {
		return relocationFileUpdate{}, false, fmt.Errorf("inspect file: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return relocationFileUpdate{}, false, fmt.Errorf("file is a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return relocationFileUpdate{}, false, fmt.Errorf("path has unsupported mode %s", info.Mode())
	}
	if info.Size() > maximumRelocationFileSize {
		return relocationFileUpdate{}, false, fmt.Errorf(
			"file is %d bytes; maximum supported size is %d bytes",
			info.Size(),
			maximumRelocationFileSize,
		)
	}
	contents, err := root.ReadFile(spec.path)
	if err != nil {
		return relocationFileUpdate{}, false, fmt.Errorf("read file: %w", err)
	}
	if !utf8.Valid(contents) {
		return relocationFileUpdate{}, false, fmt.Errorf("file is not valid UTF-8")
	}
	rewritten, err := spec.transform(contents)
	if err != nil {
		return relocationFileUpdate{}, false, err
	}
	if bytes.Equal(contents, rewritten) {
		return relocationFileUpdate{}, false, nil
	}
	return relocationFileUpdate{
		path:        spec.path,
		contents:    rewritten,
		permissions: info.Mode().Perm(),
	}, true, nil
}

func applyRelocationFiles(
	ctx context.Context,
	root *os.Root,
	updates []relocationFileUpdate,
) error {
	if len(updates) == 0 {
		return nil
	}
	defer func() {
		for _, update := range updates {
			if update.partialPath != "" {
				_ = root.Remove(update.partialPath)
			}
		}
	}()

	for index := range updates {
		if err := ctx.Err(); err != nil {
			return err
		}
		partial, partialPath, err := createPartialFile(root, updates[index].path)
		if err != nil {
			return err
		}
		updates[index].partialPath = partialPath
		_, copyErr := io.Copy(partial, contextReader{
			ctx:    ctx,
			reader: bytes.NewReader(updates[index].contents),
		})
		if copyErr != nil {
			_ = partial.Close()
			return fmt.Errorf("write staged file %s: %w", updates[index].path, copyErr)
		}
		if err := partial.Chmod(updates[index].permissions); err != nil {
			_ = partial.Close()
			return fmt.Errorf("set staged file mode %s: %w", updates[index].path, err)
		}
		if err := partial.Close(); err != nil {
			return fmt.Errorf("close staged file %s: %w", updates[index].path, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, update := range updates {
		if err := publishRegularFile(root, update.partialPath, update.path); err != nil {
			return fmt.Errorf("publish file %s: %w", update.path, err)
		}
	}
	return nil
}

func rewriteQtToolScript(
	contents []byte,
	hostToolName,
	hostToolPath string,
	windows bool,
) ([]byte, error) {
	pattern := regexp.MustCompile(
		`(?i)"?(?:[a-z]:)?[\\/](?:home|users)[\\/]qt[\\/]work[\\/]install[\\/]bin[\\/]` +
			regexp.QuoteMeta(hostToolName) + `(?:\.exe)?"?`,
	)
	matches := pattern.FindAllIndex(contents, -1)
	if len(matches) == 0 {
		if bytes.Contains(contents, []byte(hostToolPath)) {
			return contents, nil
		}
		return nil, fmt.Errorf("script contains no recognized Qt build path for %s", hostToolName)
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("script contains %d Qt build paths for %s", len(matches), hostToolName)
	}

	commandPath := quoteShellPath(hostToolPath)
	if windows {
		commandPath = `"` + hostToolPath + `"`
	}
	return pattern.ReplaceAllFunc(contents, func([]byte) []byte {
		return []byte(commandPath)
	}), nil
}

func quoteShellPath(path string) string {
	return `'` + strings.ReplaceAll(path, `'`, `'"'"'`) + `'`
}

func rewriteTargetQtConfiguration(
	contents []byte,
	kitDir,
	hostQtDir,
	hostLibraryExecutables string,
) ([]byte, error) {
	return rewriteINIValues(contents, []iniReplacement{
		{section: "Paths", key: "HostPrefix", value: filepath.ToSlash(hostQtDir)},
		{section: "Paths", key: "HostLibraryExecutables", value: hostLibraryExecutables},
		{section: "Paths", key: "HostData", value: filepath.ToSlash(kitDir)},
	})
}

func rewriteINIValues(contents []byte, replacements []iniReplacement) ([]byte, error) {
	lines := strings.SplitAfter(string(contents), "\n")
	section := ""
	for index, line := range lines {
		body, ending := splitLineEnding(line)
		trimmed := strings.TrimSpace(body)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
			continue
		}
		equals := strings.IndexByte(body, '=')
		if equals < 0 {
			continue
		}
		key := strings.TrimSpace(body[:equals])
		for replacementIndex := range replacements {
			replacement := &replacements[replacementIndex]
			if replacement.section != section || replacement.key != key {
				continue
			}
			if replacement.found {
				return nil, fmt.Errorf(
					"configuration contains duplicate [%s] %s entries",
					replacement.section,
					replacement.key,
				)
			}
			replacement.found = true
			lines[index] = replacement.key + "=" + replacement.value + ending
		}
	}

	for _, replacement := range replacements {
		if !replacement.found {
			return nil, fmt.Errorf(
				"configuration contains no [%s] %s entry",
				replacement.section,
				replacement.key,
			)
		}
	}
	return []byte(strings.Join(lines, "")), nil
}

func splitLineEnding(line string) (string, string) {
	if strings.HasSuffix(line, "\r\n") {
		return strings.TrimSuffix(line, "\r\n"), "\r\n"
	}
	if strings.HasSuffix(line, "\n") {
		return strings.TrimSuffix(line, "\n"), "\n"
	}
	return line, ""
}

func rewriteAndroidNDKHost(contents []byte, host string) ([]byte, error) {
	matches := androidNDKHostSettingPattern.FindAllIndex(contents, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("qdevice.pri contains no DEFAULT_ANDROID_NDK_HOST setting")
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf(
			"qdevice.pri contains %d DEFAULT_ANDROID_NDK_HOST settings",
			len(matches),
		)
	}
	replacement := []byte("DEFAULT_ANDROID_NDK_HOST = " + host)
	return androidNDKHostSettingPattern.ReplaceAll(contents, replacement), nil
}
