# 7-Zip Test Fixtures

`archive_extractor_test.go` embeds `t0.7z` and `empty.7z` from
`github.com/bodgit/sevenzip` version 1.6.5 as base64 strings. They exercise
regular files, empty files, and directories without requiring an external 7-Zip
binary during tests.

The fixtures are redistributed under the BSD 3-Clause License in
`LICENSE.sevenzip`.
