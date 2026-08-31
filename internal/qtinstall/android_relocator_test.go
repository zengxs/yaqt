package qtinstall

import (
	"bytes"
	"context"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

func TestAndroidRelocatorRewritesQtHostPaths(t *testing.T) {
	tests := []struct {
		name                   string
		host                   qtrepo.Host
		windows                bool
		wantNDKHost            string
		wantLibraryExecutables string
	}{
		{name: "Linux", host: qtrepo.HostLinux, wantNDKHost: "linux-x86_64", wantLibraryExecutables: "libexec"},
		{name: "Linux ARM64", host: qtrepo.HostLinuxARM64, wantNDKHost: "linux-x86_64", wantLibraryExecutables: "libexec"},
		{name: "macOS", host: qtrepo.HostMac, wantNDKHost: "darwin-x86_64", wantLibraryExecutables: "libexec"},
		{name: "Windows", host: qtrepo.HostWindows, windows: true, wantNDKHost: "windows-x86_64", wantLibraryExecutables: "bin"},
		{name: "Windows ARM64", host: qtrepo.HostWindowsARM64, windows: true, wantNDKHost: "windows-x86_64", wantLibraryExecutables: "bin"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			qtRoot, hostQtDir, kitDir := createAndroidRelocationTree(t, test.host)
			relocator, err := NewAndroidRelocator(testQtInstallationIdentity(test.host), qtRoot)
			if err != nil {
				t.Fatalf("NewAndroidRelocator() error = %v", err)
			}
			if relocator.hostQt.directory != hostQtDir {
				t.Fatalf("discovered host Qt directory = %q, want %q", relocator.hostQt.directory, hostQtDir)
			}

			if err := relocator.Relocate(context.Background(), kitDir); err != nil {
				t.Fatalf("Relocate() error = %v", err)
			}

			extension := ""
			if test.windows {
				extension = ".bat"
			}
			for wrapper, hostTool := range map[string]string{
				"qmake":    "qmake6",
				"qmake6":   "qmake6",
				"qtpaths":  "qtpaths6",
				"qtpaths6": "qtpaths6",
			} {
				contents := readRelocationFile(t, filepath.Join(kitDir, "bin", wrapper+extension))
				wantToolPath := filepath.Join(hostQtDir, "bin", hostTool)
				if test.windows {
					wantToolPath += ".exe"
					wantToolPath = strings.ReplaceAll(wantToolPath, "/", `\`)
				}
				if !strings.Contains(contents, wantToolPath) {
					t.Errorf("%s does not contain host tool path %q:\n%s", wrapper, wantToolPath, contents)
				}
				if strings.Contains(contents, "/Users/qt/work/install") {
					t.Errorf("%s still contains the Qt build path:\n%s", wrapper, contents)
				}
				info, err := os.Stat(filepath.Join(kitDir, "bin", wrapper+extension))
				if err != nil {
					t.Fatalf("stat %s: %v", wrapper, err)
				}
				if got, want := info.Mode().Perm(), fs.FileMode(0o755); got != want {
					t.Errorf("%s mode = %v, want %v", wrapper, got, want)
				}
			}

			configuration := readRelocationFile(t, filepath.Join(kitDir, "bin", "target_qt.conf"))
			for _, want := range []string{
				"[DevicePaths]\nPrefix=/usr/local/Qt-6.8.0",
				"HostPrefix=" + filepath.ToSlash(hostQtDir),
				"HostLibraryExecutables=" + test.wantLibraryExecutables,
				"HostData=" + filepath.ToSlash(kitDir),
			} {
				if !strings.Contains(configuration, want) {
					t.Errorf("target_qt.conf does not contain %q:\n%s", want, configuration)
				}
			}

			qdevice := readRelocationFile(t, filepath.Join(kitDir, "mkspecs", "qdevice.pri"))
			if want := "DEFAULT_ANDROID_NDK_HOST = " + test.wantNDKHost; !strings.Contains(qdevice, want) {
				t.Errorf("qdevice.pri does not contain %q:\n%s", want, qdevice)
			}

			before := snapshotRelocationTree(t, kitDir)
			if err := relocator.Relocate(context.Background(), kitDir); err != nil {
				t.Fatalf("second Relocate() error = %v", err)
			}
			after := snapshotRelocationTree(t, kitDir)
			for path, want := range before {
				if got := after[path]; got != want {
					t.Errorf("second Relocate() changed %s", path)
				}
			}
			assertNoRelocationPartFiles(t, kitDir)
		})
	}
}

func TestAndroidRelocatorPreflightsEveryFileBeforeWriting(t *testing.T) {
	qtRoot, _, kitDir := createAndroidRelocationTree(t, qtrepo.HostMac)
	qdevicePath := filepath.Join(kitDir, "mkspecs", "qdevice.pri")
	writeRelocationFile(t, qdevicePath, "DEFAULT_ANDROID_ABIS = arm64-v8a\n", 0o644)
	before := snapshotRelocationTree(t, kitDir)

	relocator, err := NewAndroidRelocator(testQtInstallationIdentity(qtrepo.HostMac), qtRoot)
	if err != nil {
		t.Fatalf("NewAndroidRelocator() error = %v", err)
	}
	err = relocator.Relocate(context.Background(), kitDir)
	if err == nil || !strings.Contains(err.Error(), "DEFAULT_ANDROID_NDK_HOST") {
		t.Fatalf("Relocate() error = %v, want a missing NDK host setting error", err)
	}

	after := snapshotRelocationTree(t, kitDir)
	for path, want := range before {
		if got := after[path]; got != want {
			t.Errorf("Relocate() changed %s before preflight completed", path)
		}
	}
	assertNoRelocationPartFiles(t, kitDir)
}

func TestAndroidRelocatorRejectsSymbolicLinksBeforeWriting(t *testing.T) {
	qtRoot, _, kitDir := createAndroidRelocationTree(t, qtrepo.HostMac)
	qdevicePath := filepath.Join(kitDir, "mkspecs", "qdevice.pri")
	outsidePath := filepath.Join(newTestCacheDir(t), "outside.pri")
	writeRelocationFile(t, outsidePath, "preserve\n", 0o644)
	if err := os.Remove(qdevicePath); err != nil {
		t.Fatalf("remove qdevice.pri: %v", err)
	}
	if err := os.Symlink(outsidePath, qdevicePath); err != nil {
		t.Skipf("create symbolic link: %v", err)
	}
	qmakePath := filepath.Join(kitDir, "bin", "qmake")
	qmakeBefore := readRelocationFile(t, qmakePath)

	relocator, err := NewAndroidRelocator(testQtInstallationIdentity(qtrepo.HostMac), qtRoot)
	if err != nil {
		t.Fatalf("NewAndroidRelocator() error = %v", err)
	}
	err = relocator.Relocate(context.Background(), kitDir)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Relocate() error = %v, want a symbolic link error", err)
	}
	if got := readRelocationFile(t, qmakePath); got != qmakeBefore {
		t.Errorf("qmake changed before symbolic link preflight completed")
	}
	if got, want := readRelocationFile(t, outsidePath), "preserve\n"; got != want {
		t.Errorf("outside file = %q, want %q", got, want)
	}
	assertNoRelocationPartFiles(t, kitDir)
}

func TestAndroidRelocatorHonorsCanceledContext(t *testing.T) {
	qtRoot, _, kitDir := createAndroidRelocationTree(t, qtrepo.HostLinux)
	relocator, err := NewAndroidRelocator(testQtInstallationIdentity(qtrepo.HostLinux), qtRoot)
	if err != nil {
		t.Fatalf("NewAndroidRelocator() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = relocator.Relocate(ctx, kitDir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Relocate() error = %v, want context cancellation", err)
	}
	if contents := readRelocationFile(t, filepath.Join(kitDir, "bin", "qmake")); !strings.Contains(contents, "/Users/qt/work/install") {
		t.Errorf("qmake changed after canceled relocation:\n%s", contents)
	}
}

func createAndroidRelocationTree(t *testing.T, host qtrepo.Host) (string, string, string) {
	t.Helper()
	root := newTestCacheDir(t)
	qtRoot := filepath.Join(root, "Qt")
	hostQtDir := filepath.Join(qtRoot, "6.8.0", "host")
	windows := host == qtrepo.HostWindows || host == qtrepo.HostWindowsARM64
	toolExtension := ""
	if windows {
		toolExtension = ".exe"
	}
	for _, tool := range []string{"qmake6", "qtpaths6"} {
		writeHostQtTool(t, filepath.Join(hostQtDir, "bin", tool+toolExtension), host)
	}
	writeRelocationFile(
		t,
		filepath.Join(hostQtDir, "mkspecs", "qconfig.pri"),
		"QT_VERSION = 6.8.0\nQT_MAJOR_VERSION = 6\nQT_MINOR_VERSION = 8\nQT_PATCH_VERSION = 0\n",
		0o644,
	)

	kitDir := filepath.Join(qtRoot, "6.8.0", "android_arm64_v8a")
	scriptExtension := ""
	scriptContents := func(tool string) string {
		return "#!/bin/sh\n/Users/qt/work/install/bin/" + tool + " -qtconf \"$script_dir_path/target_qt.conf\" $*\n"
	}
	if windows {
		scriptExtension = ".bat"
		scriptContents = func(tool string) string {
			return "@echo off\r\n/Users/qt/work/install/bin\\" + tool + ".exe -qtconf \"%~dp0\\target_qt.conf\" %*\r\n"
		}
	}
	for wrapper, hostTool := range map[string]string{
		"qmake":    "qmake6",
		"qmake6":   "qmake6",
		"qtpaths":  "qtpaths6",
		"qtpaths6": "qtpaths6",
	} {
		writeRelocationFile(
			t,
			filepath.Join(kitDir, "bin", wrapper+scriptExtension),
			scriptContents(hostTool),
			0o755,
		)
	}
	writeRelocationFile(t, filepath.Join(kitDir, "bin", "target_qt.conf"), `[DevicePaths]
Prefix=/usr/local/Qt-6.8.0
[Paths]
Prefix=../
HostPrefix=../../
HostBinaries=bin
HostLibraries=lib
HostLibraryExecutables=libexec
HostData=target
TargetSpec=android-clang
`, 0o644)
	writeRelocationFile(t, filepath.Join(kitDir, "mkspecs", "qdevice.pri"), `DEFAULT_ANDROID_SDK_ROOT = /opt/android/sdk
DEFAULT_ANDROID_NDK_ROOT = /opt/android/android-ndk-r26b
DEFAULT_ANDROID_NDK_HOST = darwin-x86_64
DEFAULT_ANDROID_ABIS = arm64-v8a
`, 0o644)
	return qtRoot, hostQtDir, kitDir
}

func testQtInstallationIdentity(host qtrepo.Host) qtrepo.QtInstallationIdentity {
	return qtrepo.QtInstallationIdentity{
		Host:    host,
		Version: qtrepo.Version{Major: 6, Minor: 8},
	}
}

func writeRelocationFile(t *testing.T, path, contents string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeHostQtTool(t *testing.T, path string, host qtrepo.Host) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, testHostExecutable(t, host), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func testHostExecutable(t *testing.T, host qtrepo.Host) []byte {
	t.Helper()
	var contents bytes.Buffer
	switch host {
	case qtrepo.HostMac:
		header := macho.FileHeader{
			Magic: macho.Magic64,
			Cpu:   macho.CpuArm64,
			Type:  macho.TypeExec,
		}
		if err := binary.Write(&contents, binary.LittleEndian, header); err != nil {
			t.Fatalf("encode Mach-O test executable: %v", err)
		}
		if err := binary.Write(&contents, binary.LittleEndian, uint32(0)); err != nil {
			t.Fatalf("encode Mach-O test executable reservation: %v", err)
		}
	case qtrepo.HostLinux, qtrepo.HostLinuxARM64:
		machine := elf.EM_X86_64
		if host == qtrepo.HostLinuxARM64 {
			machine = elf.EM_AARCH64
		}
		header := elf.Header64{
			Type:    uint16(elf.ET_DYN),
			Machine: uint16(machine),
			Version: uint32(elf.EV_CURRENT),
			Ehsize:  uint16(binary.Size(elf.Header64{})),
		}
		copy(header.Ident[:], elf.ELFMAG)
		header.Ident[elf.EI_CLASS] = byte(elf.ELFCLASS64)
		header.Ident[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
		header.Ident[elf.EI_VERSION] = byte(elf.EV_CURRENT)
		if err := binary.Write(&contents, binary.LittleEndian, header); err != nil {
			t.Fatalf("encode ELF test executable: %v", err)
		}
	case qtrepo.HostWindows, qtrepo.HostWindowsARM64:
		machine := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
		if host == qtrepo.HostWindowsARM64 {
			machine = pe.IMAGE_FILE_MACHINE_ARM64
		}
		dosHeader := make([]byte, 0x60)
		copy(dosHeader, "MZ")
		binary.LittleEndian.PutUint32(dosHeader[0x3c:], uint32(len(dosHeader)))
		contents.Write(dosHeader)
		contents.WriteString("PE\x00\x00")
		header := pe.FileHeader{
			Machine:         machine,
			Characteristics: pe.IMAGE_FILE_EXECUTABLE_IMAGE,
		}
		if err := binary.Write(&contents, binary.LittleEndian, header); err != nil {
			t.Fatalf("encode PE test executable: %v", err)
		}
	default:
		t.Fatalf("unsupported test host %q", host)
	}
	return contents.Bytes()
}

func readRelocationFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}

func snapshotRelocationTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[relativePath] = string(contents)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot relocation tree: %v", err)
	}
	return result
}

func assertNoRelocationPartFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".part") {
			t.Errorf("partial relocation file remains: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect relocation tree: %v", err)
	}
}
