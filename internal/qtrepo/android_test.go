package qtrepo

import "testing"

func TestParseAndroidABI(t *testing.T) {
	tests := []struct {
		value             string
		want              AndroidABI
		wantRepositoryABI string
		wantPackageArch   string
	}{
		{
			value:             "arm64-v8a",
			want:              AndroidABIArm64V8A,
			wantRepositoryABI: "arm64_v8a",
			wantPackageArch:   "android_arm64_v8a",
		},
		{
			value:             "armeabi-v7a",
			want:              AndroidABIArmeabiV7A,
			wantRepositoryABI: "armv7",
			wantPackageArch:   "android_armv7",
		},
		{
			value:             "x86",
			want:              AndroidABIX86,
			wantRepositoryABI: "x86",
			wantPackageArch:   "android_x86",
		},
		{
			value:             "x86_64",
			want:              AndroidABIX8664,
			wantRepositoryABI: "x86_64",
			wantPackageArch:   "android_x86_64",
		},
	}

	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, err := ParseAndroidABI(test.value)
			if err != nil {
				t.Fatalf("ParseAndroidABI() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("ParseAndroidABI() = %q, want %q", got, test.want)
			}
			if got := got.repositoryName(); got != test.wantRepositoryABI {
				t.Errorf("repositoryName() = %q, want %q", got, test.wantRepositoryABI)
			}
			if got := got.packageArchitecture(); got != test.wantPackageArch {
				t.Errorf("packageArchitecture() = %q, want %q", got, test.wantPackageArch)
			}
		})
	}
}

func TestParseAndroidABIRejectsRepositorySpelling(t *testing.T) {
	if _, err := ParseAndroidABI("android_arm64_v8a"); err == nil {
		t.Fatal("ParseAndroidABI() error = nil, want an unsupported ABI error")
	}
}
