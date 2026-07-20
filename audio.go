package main

import (
	"strings"

	"github.com/gordonklaus/portaudio"
	"log/slog"
)

// AudioDevice is the JSON-friendly device description sent to the frontend.
type AudioDevice struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	HostAPI    string  `json:"hostApi"`
	SampleRate float64 `json:"sampleRate"`
	IsDefault  bool    `json:"isDefault"`
	IsPulse    bool    `json:"isPulse"`
}

// listInputDevices converts PortAudio's device list into AudioDevice DTOs,
// keeping only devices with input channels. IDs are PortAudio indices.
func listInputDevices(devices []*portaudio.DeviceInfo) []AudioDevice {
	host, _ := portaudio.DefaultHostApi()
	pulseID := findPulseDevice(devices)

	out := make([]AudioDevice, 0, len(devices))
	for i, d := range devices {
		if d.MaxInputChannels == 0 {
			continue
		}
		out = append(out, AudioDevice{
			ID:         i,
			Name:       d.Name,
			HostAPI:    d.HostApi.Name,
			SampleRate: d.DefaultSampleRate,
			IsDefault:  host != nil && d == host.DefaultInputDevice,
			IsPulse:    i == pulseID,
		})
	}
	return out
}

// findPulseDevice returns the index of the PulseAudio / PipeWire input device,
// falling back to the device named "default".
// Returns -1 if neither is found.
func findPulseDevice(devices []*portaudio.DeviceInfo) int {
	for i, d := range devices {
		if strings.EqualFold(d.Name, "pulse") && d.MaxInputChannels > 0 {
			return i
		}
	}
	// PipeWire often exposes itself under "pipewire" or "default"
	for i, d := range devices {
		if strings.EqualFold(d.Name, "pipewire") && d.MaxInputChannels > 0 {
			return i
		}
	}
	for i, d := range devices {
		if strings.EqualFold(d.Name, "default") && d.MaxInputChannels > 0 {
			return i
		}
	}
	return -1
}

// pickDevice resolves the recording device index to use, in priority order:
//  1. deviceID from config (explicit user choice, if still valid)
//  2. Auto-detected PulseAudio / PipeWire device
//  3. PortAudio default (returned as -1; resolved via DefaultHostApi at record time)
func pickDevice(devices []*portaudio.DeviceInfo, deviceID int) int {
	if deviceID >= 0 {
		if deviceID < len(devices) && devices[deviceID].MaxInputChannels > 0 {
			slog.Info("[DEV] Using configured device", "id", deviceID, "name", devices[deviceID].Name)
			return deviceID
		}
		slog.Warn("[DEV] Configured device invalid (out of range or no input) — falling back", "id", deviceID)
	}

	if pulse := findPulseDevice(devices); pulse >= 0 {
		slog.Info("[DEV] Auto-selected audio device", "id", pulse, "name", devices[pulse].Name)
		return pulse
	}

	slog.Info("[DEV] No preferred device found — will use PortAudio default")
	return -1
}
