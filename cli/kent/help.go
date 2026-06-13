package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
)

//go:embed help/*.txt
var helpFS embed.FS

func writeEmbeddedUsage(fs *flag.FlagSet, name string, includeFlags bool) {
	if fs == nil {
		return
	}
	data, err := helpFS.ReadFile("help/" + name)
	if err != nil {
		panic(fmt.Sprintf("read help text %s: %v", name, err))
	}
	_, _ = io.WriteString(fs.Output(), string(data))
	if includeFlags {
		fs.PrintDefaults()
	}
}

type commandUsage struct {
	helpFile             string
	includeEmbeddedFlags bool
	parent               *commandUsage
	includeCommandFlags  bool
	title                string
	lines                []string
}

func (u commandUsage) write(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	if u.parent != nil {
		u.parent.write(fs)
		if u.includeCommandFlags {
			writeHelpSection(fs.Output(), "Flags:")
			fs.PrintDefaults()
		}
		return
	}
	if u.title != "" {
		writeHelpSection(fs.Output(), u.title, u.lines...)
		return
	}
	writeEmbeddedUsage(fs, u.helpFile, u.includeEmbeddedFlags)
}

func writeHelpSection(w io.Writer, title string, lines ...string) {
	if w == nil || title == "" {
		return
	}
	_, _ = fmt.Fprintln(w, title)
	for _, line := range lines {
		_, _ = fmt.Fprintln(w, line)
	}
	if len(lines) > 0 {
		_, _ = fmt.Fprintln(w)
	}
}

var (
	rootUsage             = commandUsage{helpFile: "root.txt", includeEmbeddedFlags: true}
	runUsage              = commandUsage{helpFile: "run.txt", includeEmbeddedFlags: true}
	sessionIDUsage        = commandUsage{helpFile: "session_id.txt"}
	goalUsage             = commandUsage{helpFile: "goal.txt"}
	goalCommandUsage      = commandUsage{parent: &goalUsage, includeCommandFlags: true}
	workflowUsage         = commandUsage{helpFile: "workflow.txt"}
	workflowCommandUsage  = commandUsage{parent: &workflowUsage, includeCommandFlags: true}
	taskUsage             = commandUsage{helpFile: "task.txt"}
	taskCommandUsage      = commandUsage{parent: &taskUsage, includeCommandFlags: true}
	projectUsage          = commandUsage{helpFile: "project.txt"}
	projectListUsage      = commandUsage{helpFile: "project_list.txt"}
	projectCreateUsage    = commandUsage{helpFile: "project_create.txt", includeEmbeddedFlags: true}
	attachUsage           = commandUsage{helpFile: "attach.txt", includeEmbeddedFlags: true}
	rebindUsage           = commandUsage{helpFile: "rebind.txt"}
	serveUsage            = commandUsage{helpFile: "serve.txt", includeEmbeddedFlags: true}
	serviceUsage          = commandUsage{helpFile: "service.txt"}
	serviceStatusUsage    = commandUsage{helpFile: "service_status.txt", includeEmbeddedFlags: true}
	serviceInstallUsage   = commandUsage{helpFile: "service_install.txt", includeEmbeddedFlags: true}
	serviceUninstallUsage = commandUsage{helpFile: "service_uninstall.txt", includeEmbeddedFlags: true}
	serviceRestartUsage   = commandUsage{helpFile: "service_restart.txt", includeEmbeddedFlags: true}
)
