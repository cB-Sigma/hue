// The entertainment streaming of github.com/cB-Sigma/hue, a module of its
// own so that the hue module keeps no dependency beyond the standard
// library.
module github.com/cB-Sigma/hue/entertainment

go 1.24.0

replace github.com/cB-Sigma/hue => ../

require (
	github.com/cB-Sigma/hue v0.3.0
	github.com/pion/dtls/v3 v3.1.10
)

require (
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/transport/v5 v5.0.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
)
