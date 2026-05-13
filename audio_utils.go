package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gordonklaus/portaudio"
)

// configDir returns ~/.config/go-desktop-transcriber, creating it if needed.
func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		dbg("[DEV] os.UserHomeDir() error: %v", err)
		return ""
	}
	dir := filepath.Join(home, ".config", "go-desktop-transcriber")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		dbg("[DEV] MkdirAll %s error: %v", dir, err)
		return ""
	}
	return dir
}

// loadSavedDevice reads the previously saved device index from disk.
// Returns -1 if not found or on any error.
func loadSavedDevice() int {
	dir := configDir()
	if dir == "" {
		return -1
	}
	path := filepath.Join(dir, "device")
	data, err := os.ReadFile(path)
	if err != nil {
		dbg("[DEV] No saved device at %s: %v", path, err)
		return -1
	}
	var id int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &id); err != nil {
		dbg("[DEV] Could not parse saved device %q: %v", strings.TrimSpace(string(data)), err)
		return -1
	}
	dbg("[DEV] Loaded saved device index: %d", id)
	return id
}

// saveDevice persists a device index to disk.
func saveDevice(id int) {
	dir := configDir()
	if dir == "" {
		return
	}
	path := filepath.Join(dir, "device")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d\n", id)), 0o644); err != nil {
		dbg("[DEV] saveDevice: os.WriteFile %s: %v", path, err)
		return
	}
	dbg("[DEV] Saved device %d to %s", id, path)
}

// findPulseDevice returns the index of the PulseAudio / PipeWire input device,
// falling back to the device named "default".
// Returns -1 if neither is found.
func findPulseDevice(devices []*portaudio.DeviceInfo) int {
	for i, d := range devices {
		if strings.EqualFold(d.Name, "pulse") && d.MaxInputChannels > 0 {
			dbg("[DEV] Found PulseAudio device at index %d: %s", i, d.Name)
			return i
		}
	}
	// PipeWire often exposes itself under "pipewire" or "default"
	for i, d := range devices {
		if strings.EqualFold(d.Name, "pipewire") && d.MaxInputChannels > 0 {
			dbg("[DEV] Found PipeWire device at index %d: %s", i, d.Name)
			return i
		}
	}
	for i, d := range devices {
		if strings.EqualFold(d.Name, "default") && d.MaxInputChannels > 0 {
			dbg("[DEV] Found 'default' ALSA device at index %d", i)
			return i
		}
	}
	return -1
}

// pickDevice resolves the recording device index to use, in priority order:
//  1. -device flag / DEVICE_ID env  (explicit user choice)
//  2. Saved device from ~/.config/go-desktop-transcriber/device
//  3. Auto-detected PulseAudio / PipeWire device
//  4. PortAudio default (returned as -1; portaudio uses its own default internally)
func pickDevice(devices []*portaudio.DeviceInfo, c *Config) int {
	dbg("[DEV] pickDevice(): TargetDevice=%d  len(devices)=%d", c.TargetDevice, len(devices))

	// 1. Explicit device
	if c.TargetDevice >= 0 {
		if c.TargetDevice < len(devices) {
			d := devices[c.TargetDevice]
			if d.MaxInputChannels > 0 {
				info("[DEV] Using requested device [%d] %s (from flag/env)", c.TargetDevice, d.Name)
				return c.TargetDevice
			}
			info("[WARN] Requested device [%d] %s has no input channels — falling back",
				c.TargetDevice, d.Name)
		} else {
			info("[WARN] Requested device %d is out of range (0..%d) — falling back",
				c.TargetDevice, len(devices)-1)
		}
	}

	// 2. Saved device
	saved := loadSavedDevice()
	if saved >= 0 && saved < len(devices) && devices[saved].MaxInputChannels > 0 {
		info("[DEV] Using saved device [%d] %s", saved, devices[saved].Name)
		return saved
	}
	if saved >= 0 {
		dbg("[DEV] Saved device %d no longer valid (out of range or no input channels)", saved)
	}

	// 3. PulseAudio / PipeWire
	pulse := findPulseDevice(devices)
	if pulse >= 0 {
		info("[DEV] Auto-selected audio device [%d] %s", pulse, devices[pulse].Name)
		return pulse
	}

	// 4. PortAudio default
	info("[DEV] No preferred device found — will use PortAudio default")
	return -1
}

// printDevices lists all available input devices to stdout.
func printDevices(devices []*portaudio.DeviceInfo) {
	fmt.Println("Available audio input devices:")
	fmt.Println()

	host, _ := portaudio.DefaultHostApi()
	savedID := loadSavedDevice()
	pulseID := findPulseDevice(devices)

	inputCount := 0
	for i, d := range devices {
		if d.MaxInputChannels == 0 {
			continue // skip output-only devices
		}
		inputCount++

		var tags []string
		if host != nil && d == host.DefaultInputDevice {
			tags = append(tags, "system-default")
		}
		if i == pulseID {
			tags = append(tags, "PulseAudio/PipeWire")
		}
		if i == savedID {
			tags = append(tags, "★ saved")
		}

		label := ""
		if len(tags) > 0 {
			label = "  [" + strings.Join(tags, ", ") + "]"
		}

		fmt.Printf("  [%d] %-45s  API: %-12s  In: %d  SR: %.0f Hz%s\n",
			i, d.Name, d.HostApi.Name, d.MaxInputChannels, d.DefaultSampleRate, label)
	}

	if inputCount == 0 {
		fmt.Println("  (no input devices found)")
	}

	fmt.Println()
	if savedID >= 0 && savedID < len(devices) {
		fmt.Printf("Currently saved: [%d] %s\n", savedID, devices[savedID].Name)
	} else {
		fmt.Println("No saved device. Auto-select will run on next start.")
	}
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  whisper-paste -set-audio-input N   — save device N for future runs")
	fmt.Println("  whisper-paste -device N            — use device N this run only")
}
