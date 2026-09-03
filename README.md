# yaqt

> Yet Another Qt Installer

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

yaqt is a command-line tool for installing Qt SDK components in automated and
non-interactive environments.


> [!WARNING]
> yaqt is a work in progress and is not yet ready for use.

## Usage

List the stable Qt versions advertised for the current platform's desktop
repository:

```console
$ yaqt list-qt
6.8.0
6.8.1
...
6.12.0
```

Select another Qt target when needed:

```console
$ yaqt list-qt --target android
$ yaqt list-qt --target ios
```

The command prints stable Qt 6.8.0 or newer releases, one version per line in
ascending order. It reads the Qt server's directory index; architecture and
package availability are validated later when a specific version is selected
for installation. Repository layout details are selected automatically from the
host and target. Android, WebAssembly, and shared Qt content are routed through
Qt's platform-independent `all_os` repository without changing the user-visible
host. The iOS repository is available only for the `mac` host.

List the package architectures advertised for one exact release:

```console
$ yaqt list-architectures 6.11.2 \
    --host windows \
    --target desktop
win64_llvm_mingw
win64_mingw
win64_msvc2022_64
win64_msvc2022_arm64_cross_compiled
```

The command supports the `desktop`, `android`, `ios`, and `wasm` repositories.
It reads the release's directory index and each package variant's `Updates.xml`,
then prints architecture identifiers with usable archive metadata in ascending
order. Android results use Qt package names such as `android_arm64_v8a`, while
`install-qt` continues to accept the standard spelling `--abi arm64-v8a`.

Architecture listing describes repository availability; it does not imply that
yaqt can complete target-specific installation and relocation for every listed
variant. The native desktop variants supported by `install-qt` are documented
below. WebAssembly installation is not implemented yet.

List the additional modules available for the current host's native desktop
package:

```console
$ yaqt list-modules 6.11.2
qtcharts
qtmultimedia
...
```

Module availability depends on the package variant. Use `--arch` to select an
explicit desktop architecture, select one Android ABI, or select the single
iOS package variant:

```console
$ yaqt list-modules 6.11.2 \
    --target android \
    --abi arm64-v8a

$ yaqt list-modules 6.11.2 --target ios
```

The command reads the corresponding main `Updates.xml` and the known Qt 6.8+
extension repositories, then prints available module names in ascending order.
Main-repository module names can be passed to the repeatable
`install-qt --module` flag. Extension modules such as `qtpdf` and
`qtwebengine` are listed when available, but installing them is not implemented
yet.

Install the native desktop Qt for the current host:

```console
$ yaqt install-qt 6.11.2 \
    --target desktop \
    --root /opt/Qt
```

The desktop architecture defaults to the native package for the selected host:

| Host | Architecture | Installation directory |
| --- | --- | --- |
| `mac` | `clang_64` | `macos` |
| `linux` | `linux_gcc_64` | `gcc_64` |
| `linux_arm64` | `linux_gcc_arm64` | `gcc_arm64` |
| `windows` | `win64_msvc2022_64` | `msvc2022_64` |
| `windows_arm64` | `win64_msvc2022_arm64` | `msvc2022_arm64` |

Use `--arch` to state that native architecture explicitly. A complete desktop
installation downloads and verifies the selected archives, extracts them under
`<root>/<version>/<installation-directory>`, writes a relative `bin/qt.conf`,
repairs package metadata that contains Qt build-machine paths, and validates the
desktop Qt tools and version before reporting success.

Install an Android Qt kit using a desktop Qt installation of the same version:

```console
$ yaqt install-qt 6.8.0 \
    --target android \
    --abi arm64-v8a \
    --root /opt/Qt
```

Install the single iOS Qt package variant on macOS:

```console
$ yaqt install-qt 6.11.2 \
    --target ios \
    --root /opt/Qt
```

The root is the directory that contains versioned Qt installations, so it must
not include the version directory itself. Before downloading a mobile Qt kit,
yaqt searches the corresponding version directory for exactly one desktop Qt
installation for the current host. That installation must contain `bin/qmake6`
and `bin/qtpaths6` (with `.exe` suffixes on Windows). yaqt does not install this
dependency automatically yet; run an explicit `--target desktop` installation
first when it is missing. yaqt reports an error if no matching desktop Qt or
multiple candidates are present.

A complete Android installation extracts each selected ABI to a directory such
as `/opt/Qt/6.8.0/android_arm64_v8a`. An iOS installation is available only for
the `mac` host and extracts its single package variant to
`/opt/Qt/6.11.2/ios`. Both targets relocate their Qt tool wrappers and
configuration files to the discovered desktop Qt. yaqt installs Qt for iOS but
does not install Xcode, Apple platform SDKs, signing certificates, or
provisioning profiles. After installation, use
`<root>/<version>/ios/bin/qtpaths6 --query` to inspect the resolved Qt paths and
`<root>/<version>/ios/bin/qt-cmake` to configure an Xcode project. See the
official [Qt for iOS guide](https://doc.qt.io/qt-6/ios.html) for Xcode and
deployment requirements.

Plan an Android installation without downloading archives or changing the Qt
installation:

```console
$ yaqt install-qt 6.8.0 \
    --target android \
    --abi arm64-v8a \
    --root /opt/Qt \
    --dry-run
```

The `--abi` flag accepts `arm64-v8a`, `armeabi-v7a`, `x86`, or `x86_64` and may
be repeated. The iOS target accepts neither `--abi` nor `--arch`. Additional Qt
modules may be selected for desktop, Android, or iOS installations with a
repeatable `--module` flag:

```console
$ yaqt install-qt 6.8.0 \
    --target android \
    --abi arm64-v8a \
    --root /opt/Qt \
    --module qtmultimedia \
    --dry-run
```

Complete installations are incremental by default. yaqt records repository
package versions in `<kit>/.yaqt/manifest.json`, where `<kit>` is a concrete
directory such as `macos`, `ios`, or `android_arm64_v8a`. Repeating a satisfied
request performs no download, extraction, or relocation. Adding a module
downloads and extracts only that module; modules omitted from a later command
remain installed. If a requested package's repository version changes, yaqt
updates that package.

Incremental installation is additive: yaqt does not uninstall omitted modules
or track ownership of individual files. A package update overlays the new
archives, so files removed from a later upstream package may remain in the kit.

When a valid existing kit has no manifest, yaqt adopts its base package instead
of extracting the base again. Existing modules cannot be inferred reliably, so
each explicitly requested module is installed once before the first manifest is
written. A manifest is published atomically only after successful path
relocation. `--download-only` and `--extract-only` never write one.

The dry run reads `Updates.xml`, groups selected archives by their base or module
package, resolves the exact archive and checksum URLs, honors archive-specific
extraction paths, reports the matching desktop Qt requirement, and labels every
selected package as `install`, `update`, `skip`, or `adopt` according to the
current kit state. It may populate or refresh the repository metadata cache.

Download and verify the selected archives without extracting them:

```console
$ yaqt install-qt 6.8.0 \
    --target android \
    --abi arm64-v8a \
    --root /opt/Qt \
    --download-only
```

Repository metadata and verified archives share one yaqt cache root. Use
`--cache-dir` with any repository command to select it, or set
`YAQT_CACHE_DIR`. Otherwise, yaqt uses the operating system's user cache
directory.

Successfully parsed directory indexes and `Updates.xml` responses are cached by
request URL under `<cache>/metadata/sha256` for 15 minutes. Expired or corrupt
entries are fetched again. Verified archives are stored by content digest under
`<cache>/downloads/sha256` and remain available until removed by the user.
Concurrent requests for the same metadata URL or archive digest share an
operating system lock, so only one process performs the corresponding download.

Stop after extraction when inspecting or diagnosing the unrelocated archive
contents:

```console
$ yaqt install-qt 6.8.0 \
    --target android \
    --abi arm64-v8a \
    --root /opt/Qt \
    --extract-only
```

The extractor accepts regular files, directories, and safe relative symbolic
links. It rejects archive paths or link targets that resolve outside the
destination, rejects other special files, and preserves safe permission and
executable bits.
This mode deliberately stops before target-specific path relocation, so its
output is not a complete Qt installation.

Complete installations and `--extract-only` operations hold a version-level
lock at `<root>/<version>/.yaqt/install.lock`. This serializes changes across
all kits of the same Qt version while still allowing different versions to be
installed concurrently. The lock file is persistent; ownership is determined
by the operating system lock, not by the file's presence. `--download-only`
does not acquire the installation lock.

## Relationship to Qt

yaqt is an independent community project. It is not affiliated with, endorsed
by, sponsored by, or supported by The Qt Company or the Qt Project. Please
report issues with yaqt to this project rather than to Qt support channels.

Qt is a registered trademark of The Qt Company Ltd. and its subsidiaries.

## Inspiration

The original idea for yaqt was inspired by
[aqtinstall](https://github.com/miurahr/aqtinstall).

## License

yaqt's own source code and documentation are licensed under the
[MIT License](LICENSE).

The MIT License for yaqt does not cover Qt SDKs, tools, documentation, examples,
or other third-party content that yaqt may download or install. Those materials
remain subject to their respective license terms. Before using or redistributing
them, review the official [Qt licensing information][qt-licensing] and
[third-party license information][qt-third-party], and ensure that you comply
with the terms that apply to your use. yaqt does not grant or modify any rights
in those materials.

[qt-licensing]: https://doc.qt.io/qt-6/licensing.html
[qt-third-party]: https://doc.qt.io/qt-6/licenses-used-in-qt.html
