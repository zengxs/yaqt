package qtinstall

import (
	"bytes"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zengxs/yaqt/internal/qtrepo"
)

const maximumHostQtConfigurationFileSize = 1 << 20

var qtVersionSettingPattern = regexp.MustCompile(
	`(?m)^QT_VERSION[ \t]*=[ \t]*([0-9]+\.[0-9]+\.[0-9]+)[ \t]*\r?$`,
)

type hostExecutableValidator func(io.ReaderAt) error

type androidHostProfile struct {
	windows                bool
	toolExtension          string
	wrapperExtension       string
	androidNDKHost         string
	hostLibraryExecutables string
	validateExecutable     hostExecutableValidator
}

type hostQtInstallation struct {
	directory string
	profile   androidHostProfile
}

var androidHostProfiles = map[qtrepo.Host]androidHostProfile{
	qtrepo.HostMac: {
		androidNDKHost:         "darwin-x86_64",
		hostLibraryExecutables: "libexec",
		validateExecutable:     validateMachOExecutable,
	},
	qtrepo.HostLinux: {
		androidNDKHost:         "linux-x86_64",
		hostLibraryExecutables: "libexec",
		validateExecutable:     validateELFAMD64Executable,
	},
	qtrepo.HostLinuxARM64: {
		androidNDKHost:         "linux-x86_64",
		hostLibraryExecutables: "libexec",
		validateExecutable:     validateELFARM64Executable,
	},
	qtrepo.HostWindows: {
		windows:                true,
		toolExtension:          ".exe",
		wrapperExtension:       ".bat",
		androidNDKHost:         "windows-x86_64",
		hostLibraryExecutables: "bin",
		validateExecutable:     validatePEAMD64Executable,
	},
	qtrepo.HostWindowsARM64: {
		windows:                true,
		toolExtension:          ".exe",
		wrapperExtension:       ".bat",
		androidNDKHost:         "windows-x86_64",
		hostLibraryExecutables: "bin",
		validateExecutable:     validatePEARM64Executable,
	},
}

func androidHostProfileFor(host qtrepo.Host) (androidHostProfile, error) {
	profile, ok := androidHostProfiles[host]
	if !ok {
		return androidHostProfile{}, fmt.Errorf("unsupported Android Qt host %q", host)
	}
	return profile, nil
}

func discoverHostQt(
	requirement qtrepo.HostQtRequirement,
	qtRoot string,
) (hostQtInstallation, error) {
	profile, err := androidHostProfileFor(requirement.Host)
	if err != nil {
		return hostQtInstallation{}, err
	}
	directory, err := findHostQtDirectory(requirement, qtRoot, profile)
	if err != nil {
		return hostQtInstallation{}, err
	}
	return hostQtInstallation{
		directory: directory,
		profile:   profile,
	}, nil
}

func (hostQt hostQtInstallation) toolPath(name string) string {
	path := filepath.Join(
		hostQt.directory,
		"bin",
		name+hostQt.profile.toolExtension,
	)
	if hostQt.profile.windows {
		return strings.ReplaceAll(path, "/", `\`)
	}
	return path
}

func findHostQtDirectory(
	requirement qtrepo.HostQtRequirement,
	qtRoot string,
	profile androidHostProfile,
) (string, error) {
	root, err := ResolveInstallRoot(qtRoot, requirement.Version)
	if err != nil {
		return "", err
	}
	versionDir := filepath.Join(root, requirement.Version.String())
	entries, err := os.ReadDir(versionDir)
	if err != nil {
		return "", fmt.Errorf("inspect Qt version directory %s: %w", versionDir, err)
	}

	candidates := make([]string, 0, 1)
	rejections := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(versionDir, entry.Name())
		if err := validateHostQtDirectory(requirement, candidate, profile); err != nil {
			if resemblesHostQtDirectory(candidate, profile) {
				rejections = append(rejections, fmt.Sprintf("%s: %v", candidate, err))
			}
			continue
		}
		candidates = append(candidates, candidate)
	}

	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		message := fmt.Sprintf(
			"no desktop Qt %s for %s found under %s",
			requirement.Version,
			requirement.Host,
			versionDir,
		)
		if len(rejections) > 0 {
			message += "; rejected candidates: " + strings.Join(rejections, "; ")
		}
		return "", errors.New(message)
	default:
		return "", fmt.Errorf(
			"multiple desktop Qt %s installations for %s found under %s: %s",
			requirement.Version,
			requirement.Host,
			versionDir,
			strings.Join(candidates, ", "),
		)
	}
}

func validateHostQtDirectory(
	requirement qtrepo.HostQtRequirement,
	hostQtDir string,
	profile androidHostProfile,
) error {
	info, err := os.Stat(hostQtDir)
	if err != nil {
		return fmt.Errorf("inspect directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory")
	}
	if _, err := os.Lstat(filepath.Join(hostQtDir, "mkspecs", "qdevice.pri")); err == nil {
		return fmt.Errorf("directory is a target Qt kit, not a desktop Qt installation")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect target Qt marker: %w", err)
	}

	for _, tool := range []string{"qmake6", "qtpaths6"} {
		toolPath := filepath.Join(hostQtDir, "bin", tool+profile.toolExtension)
		toolInfo, err := os.Stat(toolPath)
		if err != nil {
			return fmt.Errorf("inspect host Qt tool %s: %w", toolPath, err)
		}
		if !toolInfo.Mode().IsRegular() {
			return fmt.Errorf("host Qt tool %s is not a regular file", toolPath)
		}
		if !profile.windows && toolInfo.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("host Qt tool %s is not executable", toolPath)
		}
		wrapper, err := isQtToolWrapper(toolPath)
		if err != nil {
			return err
		}
		if wrapper {
			return fmt.Errorf("host Qt tool %s is a target wrapper script", toolPath)
		}
		if err := validateHostQtExecutable(toolPath, profile); err != nil {
			return fmt.Errorf(
				"host Qt tool %s does not match host %s: %w",
				toolPath,
				requirement.Host,
				err,
			)
		}
	}
	hostVersion, err := readHostQtVersion(hostQtDir)
	if err != nil {
		return err
	}
	if hostVersion != requirement.Version {
		return fmt.Errorf(
			"host Qt directory contains Qt %s, but the Android kit requires Qt %s",
			hostVersion,
			requirement.Version,
		)
	}
	return nil
}

func validateHostQtExecutable(path string, profile androidHostProfile) (resultErr error) {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open executable: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close executable: %w", err)
		}
	}()

	return profile.validateExecutable(file)
}

func validateMachOExecutable(reader io.ReaderAt) error {
	fatFile, err := macho.NewFatFile(reader)
	if err == nil {
		for _, architecture := range fatFile.Arches {
			if architecture.Type != macho.TypeExec {
				return fmt.Errorf("Mach-O image has type %s, want executable", architecture.Type)
			}
			if architecture.Cpu != macho.CpuAmd64 && architecture.Cpu != macho.CpuArm64 {
				return fmt.Errorf("Mach-O image has unsupported CPU %s", architecture.Cpu)
			}
		}
		return nil
	}
	if !errors.Is(err, macho.ErrNotFat) {
		return fmt.Errorf("parse Mach-O executable: %w", err)
	}

	thinFile, err := macho.NewFile(reader)
	if err != nil {
		return fmt.Errorf("parse Mach-O executable: %w", err)
	}
	if thinFile.Type != macho.TypeExec {
		return fmt.Errorf("Mach-O image has type %s, want executable", thinFile.Type)
	}
	if thinFile.Cpu != macho.CpuAmd64 && thinFile.Cpu != macho.CpuArm64 {
		return fmt.Errorf("Mach-O image has unsupported CPU %s", thinFile.Cpu)
	}
	return nil
}

func validateELFAMD64Executable(reader io.ReaderAt) error {
	return validateELFExecutable(reader, elf.EM_X86_64)
}

func validateELFARM64Executable(reader io.ReaderAt) error {
	return validateELFExecutable(reader, elf.EM_AARCH64)
}

func validateELFExecutable(reader io.ReaderAt, machine elf.Machine) error {
	file, err := elf.NewFile(reader)
	if err != nil {
		return fmt.Errorf("parse ELF executable: %w", err)
	}
	if file.Type != elf.ET_EXEC && file.Type != elf.ET_DYN {
		return fmt.Errorf("ELF image has type %s, want executable", file.Type)
	}
	if file.Machine != machine {
		return fmt.Errorf("ELF image has machine %s, want %s", file.Machine, machine)
	}
	return nil
}

func validatePEAMD64Executable(reader io.ReaderAt) error {
	return validatePEExecutable(reader, pe.IMAGE_FILE_MACHINE_AMD64)
}

func validatePEARM64Executable(reader io.ReaderAt) error {
	return validatePEExecutable(reader, pe.IMAGE_FILE_MACHINE_ARM64)
}

func validatePEExecutable(reader io.ReaderAt, machine uint16) error {
	file, err := pe.NewFile(reader)
	if err != nil {
		return fmt.Errorf("parse PE executable: %w", err)
	}
	if file.FileHeader.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 {
		return fmt.Errorf("PE image is not executable")
	}
	if file.FileHeader.Machine != machine {
		return fmt.Errorf(
			"PE image has machine %#x, want %#x",
			file.FileHeader.Machine,
			machine,
		)
	}
	return nil
}

func resemblesHostQtDirectory(path string, profile androidHostProfile) bool {
	if info, err := os.Stat(filepath.Join(path, "mkspecs", "qconfig.pri")); err == nil && info.Mode().IsRegular() {
		return true
	}
	info, err := os.Stat(filepath.Join(path, "bin", "qmake6"+profile.toolExtension))
	return err == nil && info.Mode().IsRegular()
}

func isQtToolWrapper(path string) (result bool, resultErr error) {
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("inspect host Qt tool contents %s: %w", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close host Qt tool %s: %w", path, err)
		}
	}()

	var prefix [16]byte
	read, err := file.Read(prefix[:])
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read host Qt tool %s: %w", path, err)
	}
	contents := bytes.ToLower(prefix[:read])
	return bytes.HasPrefix(contents, []byte("#!")) ||
		bytes.HasPrefix(contents, []byte("@echo off")), nil
}

func readHostQtVersion(hostQtDir string) (qtrepo.Version, error) {
	path := filepath.Join(hostQtDir, "mkspecs", "qconfig.pri")
	info, err := os.Stat(path)
	if err != nil {
		return qtrepo.Version{}, fmt.Errorf("inspect host Qt configuration %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return qtrepo.Version{}, fmt.Errorf("host Qt configuration %s is not a regular file", path)
	}
	if info.Size() > maximumHostQtConfigurationFileSize {
		return qtrepo.Version{}, fmt.Errorf(
			"host Qt configuration %s is %d bytes; maximum supported size is %d bytes",
			path,
			info.Size(),
			maximumHostQtConfigurationFileSize,
		)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return qtrepo.Version{}, fmt.Errorf("read host Qt configuration %s: %w", path, err)
	}
	matches := qtVersionSettingPattern.FindAllSubmatch(contents, -1)
	if len(matches) != 1 {
		return qtrepo.Version{}, fmt.Errorf(
			"host Qt configuration %s contains %d QT_VERSION settings; want exactly one",
			path,
			len(matches),
		)
	}
	version, err := qtrepo.ParseVersion(string(matches[0][1]))
	if err != nil {
		return qtrepo.Version{}, fmt.Errorf("parse host Qt version in %s: %w", path, err)
	}
	return version, nil
}
