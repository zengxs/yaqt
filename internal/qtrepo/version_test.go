package qtrepo

import "testing"

func TestParseVersion(t *testing.T) {
	version, err := ParseVersion(" 6.12.0 ")
	if err != nil {
		t.Fatalf("ParseVersion() error = %v", err)
	}
	if got, want := version.String(), "6.12.0"; got != want {
		t.Fatalf("ParseVersion() = %q, want %q", got, want)
	}
}

func TestParseVersionRejectsIncompleteVersion(t *testing.T) {
	if _, err := ParseVersion("6.8"); err == nil {
		t.Fatal("ParseVersion() error = nil, want an invalid version error")
	}
}
