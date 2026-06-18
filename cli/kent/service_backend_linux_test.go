//go:build linux

package main

import (
	"strings"
	"testing"
)

func TestSystemdUnitRoundTripsPercentInPersistenceRoot(t *testing.T) {
	// systemd specifier-expands `%` in ExecStart and the StandardOutput/Error
	// paths, so a persistence root containing `%` must be written as `%%` and read
	// back as `%`. Without the round-trip, rootMismatchError would compare a
	// different root (false mismatch) and the service would log to the wrong path.
	root := "/data/100%kent"
	spec := serviceSpec{
		Executable:    "/usr/local/bin/kent",
		Arguments:     serviceServeArguments(root),
		StdoutLogPath: root + "/service-logs/out.log",
		StderrLogPath: root + "/service-logs/err.log",
	}
	unit := renderSystemdUnit(spec)

	if strings.Contains(unit, "StandardOutput=append:"+root+"/") {
		t.Fatalf("StandardOutput path does not escape percent specifier:\n%s", unit)
	}
	if !strings.Contains(unit, "StandardOutput=append:/data/100%%kent/") {
		t.Fatalf("StandardOutput path missing escaped percent:\n%s", unit)
	}

	var execStart string
	for _, line := range strings.Split(unit, "\n") {
		if rest, ok := strings.CutPrefix(line, "ExecStart="); ok {
			execStart = rest
			break
		}
	}
	if execStart == "" {
		t.Fatalf("no ExecStart line in rendered unit:\n%s", unit)
	}
	command := parseSystemdCommand(execStart)
	got, ok := persistenceRootFromServiceCommand(command)
	if !ok || got != root {
		t.Fatalf("round-tripped persistence root = (%q, %v), want %q", got, ok, root)
	}
}
