// Test-only module: the hue client against diyHue, an independent Hue
// bridge emulator, in Docker (see interop_test.go). Not part of the
// published module.
module github.com/cB-Sigma/hue/interop

go 1.24.0

replace github.com/cB-Sigma/hue => ../

replace github.com/cB-Sigma/hue/entertainment => ../entertainment

require (
	github.com/cB-Sigma/hue v0.0.0
	github.com/cB-Sigma/hue/entertainment v0.0.0
)

require (
	github.com/pion/dtls/v3 v3.1.10 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/transport/v5 v5.0.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
)
