package qtrepo

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a stable Qt release version.
type Version struct {
	Major int
	Minor int
	Patch int
}

// ParseVersion parses a stable Qt version in MAJOR.MINOR.PATCH form.
func ParseVersion(value string) (Version, error) {
	normalized := strings.TrimSpace(value)
	parts := strings.Split(normalized, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("invalid Qt version %q (expected MAJOR.MINOR.PATCH)", value)
	}

	numbers := make([]int, len(parts))
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return Version{}, fmt.Errorf("invalid Qt version %q (expected MAJOR.MINOR.PATCH)", value)
		}
		numbers[index] = number
	}
	version := Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}
	if version.Major == 0 || version.String() != normalized {
		return Version{}, fmt.Errorf("invalid Qt version %q (expected MAJOR.MINOR.PATCH)", value)
	}
	return version, nil
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func (v Version) compare(other Version) int {
	if v.Major != other.Major {
		return compareInt(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return compareInt(v.Minor, other.Minor)
	}
	return compareInt(v.Patch, other.Patch)
}

func (v Version) compact() string {
	return fmt.Sprintf("%d%d%d", v.Major, v.Minor, v.Patch)
}

type repositoryEntry struct {
	Version   Version
	Extension string
}

var minimumSupportedVersion = Version{Major: 6, Minor: 8}

func parseRepositoryEntry(name string) (repositoryEntry, bool) {
	family, payload, ok := strings.Cut(name, "_")
	if !ok || !strings.HasPrefix(family, "qt") {
		return repositoryEntry{}, false
	}

	major, err := strconv.Atoi(strings.TrimPrefix(family, "qt"))
	if err != nil || major <= 0 {
		return repositoryEntry{}, false
	}

	encoded, extension, _ := strings.Cut(payload, "_")
	version, ok := parseCompactVersion(major, encoded)
	if !ok {
		return repositoryEntry{}, false
	}
	return repositoryEntry{Version: version, Extension: extension}, true
}

func parseCompactVersion(major int, encoded string) (Version, bool) {
	majorText := strconv.Itoa(major)
	if !strings.HasPrefix(encoded, majorText) {
		return Version{}, false
	}

	remainder := strings.TrimPrefix(encoded, majorText)
	if remainder == "" {
		return Version{}, false
	}

	minorText := remainder
	patchText := "0"
	switch len(remainder) {
	case 1:
	case 2:
		minorText = remainder[:1]
		patchText = remainder[1:]
	default:
		minorText = remainder[:2]
		patchText = remainder[2:]
	}

	minor, err := strconv.Atoi(minorText)
	if err != nil {
		return Version{}, false
	}
	patch, err := strconv.Atoi(patchText)
	if err != nil {
		return Version{}, false
	}
	return Version{Major: major, Minor: minor, Patch: patch}, true
}

func compareInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
