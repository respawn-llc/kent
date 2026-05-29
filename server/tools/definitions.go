package tools

import (
	"encoding/json"
	"sort"

	"builder/shared/toolspec"
	"builder/shared/transcript"
)

type CatalogEntry struct {
	ID             toolspec.ID
	Aliases        []string
	Description    string
	Schema         json.RawMessage
	DefaultEnabled bool
	Contract       Contract
}

var catalogEntries = []CatalogEntry{
	{
		ID:             toolspec.ToolExecCommand,
		Aliases:        []string{"bash", "bash_command", "shell", "shell_command"},
		Description:    "Runs a command in the user's default shell, returning output or a session ID for ongoing interaction.",
		DefaultEnabled: true,
		Contract: localContract(
			LocalRuntimeBuilderExecCommand,
			RequestExposure{Enabled: true},
			transcript.ToolPresentationShell,
			transcript.ToolCallRenderBehaviorShell,
			false,
			shellToolCallMeta(toolspec.ToolExecCommand),
			formatGenericToolResult,
		),
		Schema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["cmd"],
  "properties": {
    "cmd": {
      "type": "string",
      "description": "Shell command to execute."
    },
    "workdir": {
      "type": "string",
      "description": "Optional working directory to run the command in; defaults to the workspace root."
    },
    "shell": {
      "type": "string",
      "description": "Shell binary to launch. Defaults to the user's default shell."
    },
    "login": {
      "type": "boolean",
      "description": "Whether to run the shell with login semantics. Defaults to true."
    },
    "tty": {
      "type": "boolean",
      "description": "Whether to keep stdin open for follow-up write_stdin calls. Defaults to false."
    },
    "raw": {
      "type": "boolean",
      "description": "Bypass automatic optimization that reduces noise. Rerun the command in raw mode if the original output hid important details. Defaults to false."
    },
    "yield_time_ms": {
      "type": "integer",
      "description": "How long to wait for command to finish before backgrounding the process. Omit this for most commands."
    },
    "max_output_tokens": {
      "type": "integer",
      "description": "Maximum amount of output to return. Excess output will be truncated, and the full log remains available on disk. Omit this unless you want an override."
    }
  }
}`),
	},
	{
		ID:             toolspec.ToolWriteStdin,
		Aliases:        nil,
		Description:    "Writes characters to an existing exec_command session and returns recent output. Use empty chars to poll.",
		DefaultEnabled: true,
		Contract: localContract(
			LocalRuntimeBuilderWriteStdin,
			RequestExposure{Enabled: true},
			transcript.ToolPresentationShell,
			transcript.ToolCallRenderBehaviorShell,
			false,
			shellToolCallMeta(toolspec.ToolWriteStdin),
			formatGenericToolResult,
		),
		Schema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["session_id"],
  "properties": {
    "session_id": {
      "type": "integer",
      "description": "Identifier of the running exec_command session."
    },
    "chars": {
      "type": "string",
      "description": "Bytes to write to stdin. May be empty to poll for output."
    },
    "yield_time_ms": {
      "type": "integer",
      "description": "How long to wait in milliseconds for output before yielding."
    },
    "max_output_tokens": {
      "type": "integer",
      "description": "Optional maximum amount of output to return back. Excess output will be truncated."
    }
  }
}`),
	},
	{
		ID:             toolspec.ToolViewImage,
		Aliases:        []string{"read_image"},
		Description:    "View a local PNG, JPEG, still GIF, or PDF file by path. Images may be compressed before model input unless raw=true. You will see PDFs as images (not OCR/text).",
		DefaultEnabled: true,
		Contract: localContract(
			LocalRuntimeBuilderViewImage,
			RequestExposure{Enabled: true, RequiresVision: true},
			transcript.ToolPresentationDefault,
			transcript.ToolCallRenderBehaviorDefault,
			false,
			defaultToolCallMeta(toolspec.ToolViewImage),
			formatViewImageToolResult,
		),
		Schema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path"],
  "properties": {
    "path": {
      "type": "string",
      "description": "Local filesystem path to a PNG, JPEG, still GIF, or PDF file. Relative paths resolve from the workspace root."
    },
    "raw": {
      "type": "boolean",
      "description": "Whether to bypass image compression and postprocessing. Defaults to false. The file size cap still applies."
    }
  }
}`),
	},
	{
		ID:             toolspec.ToolPatch,
		Aliases:        nil,
		Description:    "Apply edits to files using freeform patch syntax.",
		DefaultEnabled: true,
		Contract: localContract(
			LocalRuntimeBuilderPatch,
			RequestExposure{Enabled: true},
			transcript.ToolPresentationDefault,
			transcript.ToolCallRenderBehaviorDefault,
			true,
			patchToolCallMeta(toolspec.ToolPatch),
			formatPatchToolResult,
		),
		Schema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["patch"],
  "properties": {
    "patch": {
      "type": "string",
      "description": "Patch text in freeform format."
    }
  }
}`),
	},
	{
		ID:             toolspec.ToolEdit,
		Aliases:        []string{"replace", "write"},
		Description:    "Replace text in a file, create a missing or empty file, or delete matched text. old_string should match current file content and include enough context to be unique.",
		DefaultEnabled: false,
		Contract: localContract(
			LocalRuntimeBuilderEdit,
			RequestExposure{Enabled: true},
			transcript.ToolPresentationDefault,
			transcript.ToolCallRenderBehaviorDefault,
			true,
			editToolCallMeta(toolspec.ToolEdit),
			formatEditToolResult,
		),
		Schema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["path", "old_string", "new_string"],
  "properties": {
    "path": {
      "type": "string",
      "description": "File path to edit. Relative paths resolve from the workspace root; absolute paths are allowed."
    },
    "old_string": {
      "type": "string",
      "description": "Exact current text to replace. Include enough surrounding context to make the match unique. Use an empty string only to create a missing or empty file."
    },
    "new_string": {
      "type": "string",
      "description": "Replacement text. Use an empty string to delete the matched text."
    },
    "replace_all": {
      "type": "boolean",
      "description": "Replace all occurrences of the selected match. Defaults to false."
    }
  }
}`),
	},
	{
		ID:             toolspec.ToolAskQuestion,
		Aliases:        nil,
		Description:    "Ask the user a question. You should ask the user when planning or working to make product decisions, resolve ambiguities, define missing pieces that you cannot resolve by yourself, brainstorming with the user. You should ask the user a lot of questions when you're planning/brainstorming together to learn their desires, preferences, design, product vision, architecture, and sometimes ask them questions when already working if you encounter a problem you can't resolve, a caveat, an undefined area that materially affects the result or direction of your work, etc. You should avoid asking the user obvious or harmless questions like 'Should I run tests?' or 'Where is file X?' which you can answer yourself. Stick to ONE question per this tool call, for multiple questions call this tool in parallel. Strive to provide multiple suggestions/options with every question if applicable, and providing one recommended option you deem best for user goals.",
		DefaultEnabled: true,
		Contract: localContract(
			LocalRuntimeBuilderAskQuestion,
			RequestExposure{Enabled: true},
			transcript.ToolPresentationAskQuestion,
			transcript.ToolCallRenderBehaviorAskQuestion,
			false,
			askQuestionToolCallMeta(toolspec.ToolAskQuestion),
			formatAskQuestionToolResult,
		),
		Schema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["question"],
  "properties": {
    "question": {
      "type": "string",
      "description": "Question text shown to the user. You must only put exactly ONE question here."
    },
    "suggestions": {
      "type": "array",
      "description": "Optional choice suggestions. Omit this field when you want a freeform-only answer. If you provide >1 suggestions, provide recommended_option_index. Strive to give users the best, sensible options possible, following best-practices, guidelines, and common sense.",
      "items": {"type": "string"}
    },
    "recommended_option_index": {
      "type": "integer",
      "description": "Optional 1-based index of the recommended suggestion."
    }
  }
}`),
	},
	{
		ID:             toolspec.ToolCompleteNode,
		Aliases:        nil,
		Description:    "Complete the current workflow node. Use this only in workflow tool-completion mode, exactly once when the node work is done.",
		DefaultEnabled: false,
		Contract: localContract(
			LocalRuntimeBuilderCompleteNode,
			RequestExposure{Enabled: true, RequiresWorkflowRun: true},
			transcript.ToolPresentationDefault,
			transcript.ToolCallRenderBehaviorDefault,
			false,
			defaultToolCallMeta(toolspec.ToolCompleteNode),
			formatGenericToolResult,
		),
		// Runtime requests replace this fallback with the current workflow
		// run contract, including valid transition IDs and node output fields.
		Schema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "transition_id": {
      "type": "string",
      "description": "Transition ID to take. Required when multiple outgoing transitions are available."
    },
    "commentary": {
      "type": "string",
      "description": "Brief explanation of what was completed and why this transition was selected."
    }
  }
}`),
	},
	{
		ID:             toolspec.ToolTriggerHandoff,
		Aliases:        nil,
		Description:    "Trigger a proactive handoff to another agent. By default, this tool is disallowed even if visible. Using this tool is allowed only after a specific developer message appears in transcript that allows this tool. Do not use this tool before the reminder. The tool is private to you, so you can use 'analysis' channel content in its parameters.",
		DefaultEnabled: false,
		Contract: localContract(
			LocalRuntimeBuilderTriggerHandoff,
			RequestExposure{Enabled: true},
			transcript.ToolPresentationDefault,
			transcript.ToolCallRenderBehaviorDefault,
			false,
			triggerHandoffToolCallMeta(toolspec.ToolTriggerHandoff),
			formatTriggerHandoffToolResult,
		),
		Schema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "summarizer_prompt": {
      "type": "string",
      "description": "Optional *extra* instructions for the handoff summarizer. The summarizer already receives detailed generic guidance on preserving the workspace state and full conversation transcript. Only use this to add something specific about your current thoughts or state of work."
    },
    "future_agent_message": {
      "type": "string",
      "description": "Optional message to forward verbatim to the next agent *in addition* to the detailed summary of current work. Only include here specific concise information to preserve from the analysis block or the next immediate step, not generic guidance or converstaion summary."
    }
  }
}`),
	},
	{
		ID:             toolspec.ToolWebSearch,
		Aliases:        nil,
		Description:    "Search the web for up-to-date external information. Use this when local workspace context is insufficient or the fact could be stale, or for information beyond your model knowledge cutoff. Prefer primary and official sources.",
		DefaultEnabled: true,
		Contract: hostedContract(
			RequestExposure{Enabled: false},
			transcript.ToolPresentationDefault,
			transcript.ToolCallRenderBehaviorDefault,
			false,
			true,
			webSearchToolCallMeta(toolspec.ToolWebSearch),
			formatWebSearchToolResult,
			decodeHostedWebSearchOutput,
		),
		Schema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["query"],
  "properties": {
    "query": {
      "type": "string",
      "description": "Required search query string. Keep it specific and concise; include concrete keywords (entity + property + timeframe) and optionally a site hint."
    },
    "allowed_domains": {
      "type": "array",
      "description": "Optional allowlist of domains to constrain sources to preferred/authoritative sites.",
      "items": {"type": "string"}
    },
    "blocked_domains": {
      "type": "array",
      "description": "Optional blocklist of domains to exclude low-quality or irrelevant sources.",
      "items": {"type": "string"}
    }
  }
}`),
	},
}

var (
	definitions       map[toolspec.ID]Definition
	parseAliases      map[string]toolspec.ID
	catalogIDs        []toolspec.ID
	defaultEnabledIDs []toolspec.ID
)

func init() {
	definitions = make(map[toolspec.ID]Definition, len(catalogEntries))
	parseAliases = make(map[string]toolspec.ID, len(catalogEntries)*2)
	catalogIDs = make([]toolspec.ID, 0, len(catalogEntries))
	defaultEnabledIDs = make([]toolspec.ID, 0, len(catalogEntries))

	for _, entry := range catalogEntries {
		validateCatalogEntry(entry)
		definitions[entry.ID] = Definition{
			ID:          entry.ID,
			Description: entry.Description,
			Schema:      entry.Schema,
			contract:    entry.Contract,
		}
		parseAliases[string(entry.ID)] = entry.ID
		for _, alias := range entry.Aliases {
			parseAliases[alias] = entry.ID
		}
		catalogIDs = append(catalogIDs, entry.ID)
		if entry.DefaultEnabled {
			defaultEnabledIDs = append(defaultEnabledIDs, entry.ID)
		}
	}

	sort.Slice(catalogIDs, func(i, j int) bool { return catalogIDs[i] < catalogIDs[j] })
	sort.Slice(defaultEnabledIDs, func(i, j int) bool { return defaultEnabledIDs[i] < defaultEnabledIDs[j] })
}

func Catalog() []CatalogEntry {
	out := make([]CatalogEntry, len(catalogEntries))
	copy(out, catalogEntries)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func CatalogIDs() []toolspec.ID {
	out := make([]toolspec.ID, len(catalogIDs))
	copy(out, catalogIDs)
	return out
}

func DefaultEnabledToolIDs() []toolspec.ID {
	out := make([]toolspec.ID, len(defaultEnabledIDs))
	copy(out, defaultEnabledIDs)
	return out
}

func parseCatalogID(v string) (toolspec.ID, bool) {
	id, ok := parseAliases[v]
	return id, ok
}

func definitionFor(id toolspec.ID) (Definition, bool) {
	def, ok := definitions[id]
	return def, ok
}

func validateCatalogEntry(entry CatalogEntry) {
	if entry.Contract.Runtime.Availability == "" {
		panic("tool contract is missing runtime availability for " + string(entry.ID))
	}
	if entry.Contract.Runtime.Availability == RuntimeAvailabilityHosted && entry.Contract.Runtime.DecodeHostedOutput == nil {
		panic("hosted tool contract is missing hosted output decoder for " + string(entry.ID))
	}
	if entry.Contract.Runtime.Availability == RuntimeAvailabilityLocal && entry.Contract.Runtime.LocalBuilder == "" {
		panic("local tool contract is missing local runtime builder for " + string(entry.ID))
	}
	if entry.Contract.Runtime.Availability == RuntimeAvailabilityHosted && entry.Contract.Runtime.LocalBuilder != "" {
		panic("hosted tool contract must not declare a local runtime builder for " + string(entry.ID))
	}
	if entry.Contract.Transcript.BuildCallMeta == nil {
		panic("tool contract is missing transcript call metadata builder for " + string(entry.ID))
	}
	if entry.Contract.Transcript.FormatResult == nil {
		panic("tool contract is missing transcript result formatter for " + string(entry.ID))
	}
	if entry.Contract.Transcript.Presentation == "" {
		panic("tool contract is missing transcript presentation for " + string(entry.ID))
	}
	if entry.Contract.Transcript.RenderBehavior == "" {
		panic("tool contract is missing transcript render behavior for " + string(entry.ID))
	}
}
