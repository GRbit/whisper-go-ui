// Package icons embeds the tray-state PNGs.
package icons

import _ "embed"

//go:generate go run ./gen

//go:embed waiting.png
var Waiting []byte

//go:embed recording.png
var Recording []byte

//go:embed transcribing.png
var Transcribing []byte

//go:embed pasted.png
var Pasted []byte
