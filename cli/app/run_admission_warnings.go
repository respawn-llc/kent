package app

import (
	"fmt"
	"strings"
	"sync"

	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/serverapi"
)

type RunAgentFlag uint8

const (
	RunAgentFlagAgent RunAgentFlag = iota
	RunAgentFlagFast
)

func RunAdmissionWarnings(accepted *runpromptpb.ActiveSteerAccepted, agentFlag RunAgentFlag) ([]string, error) {
	if accepted == nil {
		return nil, nil
	}
	warnings := []string{"Note: session was already running, your message was steered instead"}
	var flags []string
	for _, setting := range accepted.IgnoredSettings {
		switch setting {
		case runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_AGENT:
			switch agentFlag {
			case RunAgentFlagAgent:
				flags = append(flags, "--agent")
			case RunAgentFlagFast:
				flags = append(flags, "--fast")
			default:
				return nil, fmt.Errorf("invalid Run agent flag %d", agentFlag)
			}
		case runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_MODEL:
			flags = append(flags, "--model")
		case runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_TOOLS:
			flags = append(flags, "--tools")
		case runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_THEME:
			flags = append(flags, "--theme")
		case runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_MODEL_TIMEOUT:
			flags = append(flags, "--model-timeout-seconds")
		default:
			return nil, fmt.Errorf("invalid ignored Run setting %d", setting)
		}
	}
	if len(flags) != 0 {
		warnings = append(warnings, "Some arguments were ignored as the running session already had those settings set. Changing them would invalidate caches. Ignored options: "+strings.Join(flags, ", "))
	}
	return warnings, nil
}

type runAdmissionCollector struct {
	mu       sync.Mutex
	accepted *runpromptpb.ActiveSteerAccepted
	progress serverapi.RunPromptProgressSink
}

func (c *runAdmissionCollector) PublishRunPromptProgress(event *runpromptpb.ProgressEvent) {
	if accepted := event.GetActiveSteerAccepted(); accepted != nil {
		c.mu.Lock()
		c.accepted = accepted
		c.mu.Unlock()
	}
	if c.progress != nil {
		c.progress.PublishRunPromptProgress(event)
	}
}

func (c *runAdmissionCollector) warnings(flag RunAgentFlag) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return RunAdmissionWarnings(c.accepted, flag)
}
