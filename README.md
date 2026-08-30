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
```

The command prints stable Qt 6.8.0 or newer releases, one version per line in
ascending order. It reads the Qt server's directory index; architecture and
package availability are validated later when a specific version is selected
for installation. Repository layout details are selected automatically from the
host and target. Android, WebAssembly, and shared Qt content are routed through
Qt's platform-independent `all_os` repository without changing the user-visible
host.

Plan an Android installation without downloading or changing any files:

```console
$ yaqt install-qt 6.8.0 \
    --target android \
    --abi arm64-v8a \
    --output-dir /opt/Qt \
    --dry-run
```

The `--abi` flag accepts `arm64-v8a`, `armeabi-v7a`, `x86`, or `x86_64` and may
be repeated. Additional Qt modules may be selected with a repeatable `--module`
flag:

```console
$ yaqt install-qt 6.8.0 \
    --target android \
    --abi arm64-v8a \
    --module qtmultimedia \
    --dry-run
```

The dry run reads `Updates.xml`, groups selected archives by their base or module
package, resolves the exact archive and checksum URLs, honors archive-specific
extraction paths, and reports the matching desktop Qt requirement.

Download and verify the selected archives without extracting them:

```console
$ yaqt install-qt 6.8.0 \
    --target android \
    --abi arm64-v8a \
    --download-only
```

Verified archives are stored in a content-addressed cache. Use `--cache-dir` to
select its root, or set `YAQT_CACHE_DIR`. Otherwise, yaqt uses the operating
system's user cache directory.

An explicit extraction-only mode is available while the Android installation
workflow is under development:

```console
$ yaqt install-qt 6.8.0 \
    --target android \
    --abi arm64-v8a \
    --output-dir /opt/Qt \
    --extract-only
```

The extractor accepts regular files and directories, rejects unsafe archive
paths and special files, and preserves safe permission and executable bits.
This mode stops before Android path relocation, so its output is not yet a
complete Qt installation.

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
