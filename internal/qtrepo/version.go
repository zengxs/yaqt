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
