package ongoing

import transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

type TerminalWork interface{ terminalWork() }

type TerminalMessage struct{ Message *transcriptpb.Message }
type TerminalReset struct{ Reason RehydrateReason }

func (TerminalMessage) terminalWork() {}
func (TerminalReset) terminalWork()   {}
