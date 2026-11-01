package app

import (
	"core/cli/tui/ongoing"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

type askDocumentRange struct {
	start int
	end   int
}

type askDocument struct {
	lines   []uiInputPaneContentLine
	cursor  uiInputFieldCursor
	options map[int]askDocumentRange
	editor  *askDocumentRange
}

func (d askDocument) viewport(scroll, capacity int, focus askAnswerFocus) askInputViewport {
	start := min(scroll, max(0, len(d.lines)-capacity))
	end := min(len(d.lines), start+capacity)
	viewport := askInputViewport{lines: d.lines[start:end]}
	if focus == askFocusAnswer && d.cursor.Visible && d.cursor.Row >= start && d.cursor.Row < end {
		viewport.cursor = d.cursor
		viewport.cursor.Row -= start
	}
	return viewport
}

func (l uiViewLayout) askDocument(width int) askDocument {
	document := askDocument{options: make(map[int]askDocumentRange)}
	if projection := l.model.ask.activeProjection; projection != nil {
		for _, text := range projection.rows {
			document.lines = append(document.lines, uiInputPaneContentLine{
				text: text, prompt: askPromptLine{Kind: askPromptLineKindQuestion},
			})
		}
	}
	priority, cursor := l.askPriorityPaneContent(width)
	if cursor.Visible {
		cursor.Row += len(document.lines)
	}
	document.cursor = cursor
	document.lines = append(document.lines, priority...)
	for index := range document.lines {
		line := &document.lines[index]
		line.documentRow = index
		if option := line.prompt.Option; option != nil {
			rows, exists := document.options[*option]
			if !exists {
				rows.start = index
			}
			rows.end = index + 1
			document.options[*option] = rows
		}
		if line.prompt.Kind == askPromptLineKindInput {
			if document.editor == nil {
				document.editor = &askDocumentRange{start: index}
			}
			document.editor.end = index + 1
		}
	}
	return document
}

func (m *uiModel) askFitsInline(message *transcriptpb.Message) bool {
	layout := m.layout()
	width, height := layout.effectiveWidth(), layout.effectiveHeight()
	document := layout.askDocument(width)
	viewport := layout.askInputViewport(width, inputContentLineLimit(height))
	if len(viewport.lines) != len(document.lines) {
		return false
	}
	for index, line := range viewport.lines {
		if line.documentRow != index {
			return false
		}
	}
	frame := m.ongoingFrameInput()
	var messages []*transcriptpb.Message
	if m.ongoingTranscript != nil {
		frame = m.ongoingTranscript.composeFrame(frame)
		messages = append(messages, m.ongoingTranscript.queue...)
	}
	// Tests and non-native embeddings have no assistant tail, but still use
	// exactly the same final allocation policy.
	surface := m.ongoingSurface
	if surface == nil {
		surface = ongoing.NewSurface()
	}
	if message != nil && !isAppOwnedOngoingMessage(message.Event) {
		messages = append(messages, message)
	}
	if len(messages) == 0 {
		return askAllocationCoversDocument(surface.AllocateFrame(frame), document)
	}
	for allocation := range surface.AllocateMessageFrames(messages, frame) {
		if !askAllocationCoversDocument(allocation, document) {
			return false
		}
	}
	return true
}

func askAllocationCoversDocument(allocation ongoing.FrameAllocation, document askDocument) bool {
	rows := allocation.SectionRows(ongoing.FrameSectionInput)
	if len(rows) != len(document.lines)+2 {
		return false
	}
	for index, row := range rows {
		if row != index+1 {
			return false
		}
	}
	return true
}
