//go:build windows

package main

import (
	brand "core/shared/config"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

func TestWindowsServiceRegistrationAllowsRefresh(t *testing.T) {
	spec := serviceSpec{
		Executable: `C:\Program Files\Kent\kent.exe`,
		Config:     brand.App{PersistenceRoot: `C:\Users\Nek\.kent`},
	}
	existing := windowsServiceConfig(spec)
	existing.BinaryPathName = windows.ComposeCommandLine([]string{
		spec.Executable, "service", "run", "--persistence-root", spec.Config.PersistenceRoot,
	})
	if err := validateWindowsServiceRegistration(existing, spec); err != nil {
		t.Fatalf("same registration must permit refresh without force: %v", err)
	}
	t.Run("equivalent command quoting", func(t *testing.T) {
		cfg := existing
		cfg.BinaryPathName = `"C:\Program Files\Kent\kent.exe" "service" "run" "--persistence-root" "C:\Users\Nek\.kent"`
		if err := validateWindowsServiceRegistration(cfg, spec); err != nil {
			t.Fatalf("equivalent command must permit refresh: %v", err)
		}
	})
	for _, tc := range []struct {
		name   string
		change func(*mgr.Config)
	}{
		{"different executable", func(c *mgr.Config) {
			c.BinaryPathName = windows.ComposeCommandLine([]string{`C:\Other\kent.exe`, "service", "run", "--persistence-root", spec.Config.PersistenceRoot})
		}},
		{"different persistence root", func(c *mgr.Config) {
			c.BinaryPathName = windows.ComposeCommandLine([]string{spec.Executable, "service", "run", "--persistence-root", `C:\Users\Other\.kent`})
		}},
		{"different arguments", func(c *mgr.Config) { c.BinaryPathName += " --unexpected" }},
		{"different account", func(c *mgr.Config) { c.ServiceStartName = "OtherUser" }},
		{"different start mode", func(c *mgr.Config) { c.StartType = windows.SERVICE_DEMAND_START }},
		{"different service type", func(c *mgr.Config) { c.ServiceType = windows.SERVICE_WIN32_SHARE_PROCESS }},
		{"different error control", func(c *mgr.Config) { c.ErrorControl = windows.SERVICE_ERROR_IGNORE }},
		{"different display name", func(c *mgr.Config) { c.DisplayName = "Other service" }},
		{"different description", func(c *mgr.Config) { c.Description = "Other description" }},
		{"different dependencies", func(c *mgr.Config) { c.Dependencies = []string{"OtherService"} }},
		{"different load group", func(c *mgr.Config) { c.LoadOrderGroup = "OtherGroup" }},
		{"different SID mode", func(c *mgr.Config) { c.SidType = windows.SERVICE_SID_TYPE_UNRESTRICTED }},
		{"delayed startup", func(c *mgr.Config) { c.DelayedAutoStart = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := existing
			tc.change(&cfg)
			if err := validateWindowsServiceRegistration(cfg, spec); err == nil {
				t.Fatal("different registration must require force")
			}
		})
	}
}
