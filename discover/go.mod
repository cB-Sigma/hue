// Discovery of Hue bridges for github.com/cB-Sigma/hue, a module of its
// own so that the hue module keeps no dependency beyond the standard
// library.
module github.com/cB-Sigma/hue/discover

go 1.27.1

replace github.com/cB-Sigma/hue => ../

require (
	github.com/cB-Sigma/hue v0.2.0
	github.com/cB-Sigma/mdns v0.2.0
)

require (
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)
