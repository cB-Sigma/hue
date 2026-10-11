// Test-only module: the hue client against diyHue, an independent Hue
// bridge emulator, in Docker (see interop_test.go). Not part of the
// published module.
module github.com/cB-Sigma/hue/interop

go 1.24

replace github.com/cB-Sigma/hue => ../

require github.com/cB-Sigma/hue v0.0.0
