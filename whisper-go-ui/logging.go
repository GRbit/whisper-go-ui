package main

import (
	"log"
	"sync/atomic"
)

// debugMode mirrors Config.Debug; atomic so any goroutine can read it cheaply
// and SaveConfig can flip it live.
var debugMode atomic.Bool

// dbg prints a debug line when debugMode is on.
// Caller is responsible for a meaningful [TAG] prefix.
func dbg(format string, args ...interface{}) {
	if debugMode.Load() {
		log.Printf("[DBG] "+format, args...)
	}
}

// info always prints — normal operational messages.
func info(format string, args ...interface{}) {
	log.Printf(format, args...)
}
