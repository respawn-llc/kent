package ongoing

import transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

type TerminalWork interface{ terminalWork() }

type TerminalMessage struct{ Message *transcriptpb.Message }
type TerminalHistory struct{ Rows []*transcriptpb.CommittedRow }
type TerminalReset struct{ Reason RehydrateReason }

func (TerminalMessage) terminalWork() {}
func (TerminalHistory) terminalWork() {}
func (TerminalReset) terminalWork()   {}
