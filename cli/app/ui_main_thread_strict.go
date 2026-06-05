package app

import (
	"fmt"
	"os"
	"strings"
)

type tuiStrictIOMode string

const (
	tuiStrictIOModeOff   tuiStrictIOMode = "off"
	tuiStrictIOModeLog   tuiStrictIOMode = "log"
	tuiStrictIOModePanic tuiStrictIOMode = "panic"
)

type uiMainThreadState struct {
	depth    int
	activity string
}

var defaultTUIStrictIOMode = tuiStrictIOModeLog

func parseTUIStrictIOMode(value string) (tuiStrictIOMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", false
	case string(tuiStrictIOModeOff), "0", "false", "no":
		return tuiStrictIOModeOff, true
	case string(tuiStrictIOModeLog), "warn", "warning", "1", "true", "yes":
		return tuiStrictIOModeLog, true
	case string(tuiStrictIOModePanic), "crash", "fail":
		return tuiStrictIOModePanic, true
	default:
		return "", false
	}
}

func initialTUIStrictIOMode(debug bool) (tuiStrictIOMode, bool) {
	if mode, ok := parseTUIStrictIOMode(os.Getenv("BUILDER_TUI_STRICT_IO")); ok {
		return mode, true
	}
	if debug {
		return tuiStrictIOModePanic, false
	}
	return defaultTUIStrictIOMode, false
}

func (m *uiModel) enterUIMainThread(activity string) func() {
	if m == nil {
		return func() {}
	}
	previous := m.uiMainThread.activity
	m.uiMainThread.depth++
	m.uiMainThread.activity = strings.TrimSpace(activity)
	return func() {
		if m.uiMainThread.depth > 0 {
			m.uiMainThread.depth--
		}
		if m.uiMainThread.depth == 0 {
			m.uiMainThread.activity = previous
		}
	}
}

func (m *uiModel) checkTUIBlockingOperation(kind, detail string) {
	if m == nil || m.uiMainThread.depth <= 0 {
		return
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "blocking operation"
	}
	detail = strings.TrimSpace(detail)
	message := fmt.Sprintf("TUI main-thread I/O violation during %s: %s", m.uiMainThread.activity, kind)
	if detail != "" {
		message += " (" + detail + ")"
	}
	switch m.tuiStrictIOMode {
	case tuiStrictIOModePanic:
		panic(message)
	case tuiStrictIOModeLog:
		m.logf("%s", message)
	}
}
