package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
)

//go:embed help/*.txt
var builderHelpFS embed.FS

func writeEmbeddedUsage(fs *flag.FlagSet, name string, includeFlags bool) {
	if fs == nil {
		return
	}
	data, err := builderHelpFS.ReadFile("help/" + name)
	if err != nil {
		panic(fmt.Sprintf("read help text %s: %v", name, err))
	}
	_, _ = io.WriteString(fs.Output(), string(data))
	if includeFlags {
		fs.PrintDefaults()
	}
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

func writeUsageWithFlags(fs *flag.FlagSet, writeUsage func(*flag.FlagSet)) {
	writeUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeRootUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "root.txt", true)
}

func writeRunUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "run.txt", true)
}

func writeSessionIDUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "session_id.txt", false)
}

func writeGoalUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "goal.txt", false)
}

func writeGoalCommandUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeGoalUsage)
}

func writeWorkflowUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "workflow.txt", false)
}

func writeWorkflowCreateUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeWorkflowUsage)
}

func writeWorkflowNodeAddUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeWorkflowUsage)
}

func writeWorkflowNodeUpdateUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeWorkflowUsage)
}

func writeWorkflowEdgeAddUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeWorkflowUsage)
}

func writeWorkflowEdgeUpdateUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeWorkflowUsage)
}

func writeWorkflowLinkUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeWorkflowUsage)
}

func writeWorkflowValidateUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeWorkflowUsage)
}

func writeTaskUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "task.txt", false)
}

func writeTaskCreateUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeTaskUsage)
}

func writeTaskStartUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeTaskUsage)
}

func writeTaskResumeUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeTaskUsage)
}

func writeTaskApproveUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeTaskUsage)
}

func writeTaskMoveUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeTaskUsage)
}

func writeTaskListUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeTaskUsage)
}

func writeTaskShowUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeTaskUsage)
}

func writeTaskCancelUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeTaskUsage)
}

func writeTaskCommentUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
}

func writeTaskCommentAddUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeTaskUsage)
}

func writeTaskCommentListUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeTaskUsage)
}

func writeTaskCommentReplaceUsage(fs *flag.FlagSet) {
	writeUsageWithFlags(fs, writeTaskUsage)
}

func writeTaskCommentDeleteUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
}

func writeProjectUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "project.txt", false)
}

func writeProjectListUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "project_list.txt", false)
}

func writeProjectCreateUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "project_create.txt", true)
}

func writeAttachUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "attach.txt", true)
}

func writeRebindUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "rebind.txt", false)
}

func writeServeUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "serve.txt", true)
}

func writeServiceUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "service.txt", false)
}

func writeServiceStatusUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "service_status.txt", true)
}

func writeServiceInstallUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "service_install.txt", true)
}

func writeServiceUninstallUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "service_uninstall.txt", true)
}

func writeServiceLifecycleUsage(fs *flag.FlagSet, action serviceAction) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder service "+string(action)+":",
		"  builder service "+string(action),
	)
}

func writeServiceRestartUsage(fs *flag.FlagSet) {
	writeEmbeddedUsage(fs, "service_restart.txt", true)
}
