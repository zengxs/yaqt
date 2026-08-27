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

Select another repository explicitly when needed:

```console
$ yaqt list-qt --host all_os --target wasm
```

The command prints stable Qt 6.8.0 or newer releases, one version per line in
ascending order. It reads the Qt server's directory index; architecture and
package availability are validated later when a specific version is selected
for installation.

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
