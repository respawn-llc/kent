package app

import (
	"fmt"
	"io"

	xansi "github.com/charmbracelet/x/ansi"
)

type uiTerminalKeyboardReportingOwner struct {
	output       *uiTerminalOutput
	enabled      bool
	started      bool
	acquired     bool
	suspended    bool
	closed       bool
	screenParser rendererOutputGateAltScreenParser
}

func newUITerminalKeyboardReportingOwner(output *uiTerminalOutput, enabled bool) *uiTerminalKeyboardReportingOwner {
	return &uiTerminalKeyboardReportingOwner{
		output:  output,
		enabled: enabled,
	}
}

func (o *uiTerminalKeyboardReportingOwner) writeLocked(output io.Writer, payload []byte) (int, error) {
	if !o.enabled || o.closed {
		return output.Write(payload)
	}

	nextParser, transition := o.screenParser.preview(payload)
	if !o.suspended && !o.started {
		if err := writeKittyKeyboardReporting(output, true); err != nil {
			return 0, err
		}
		o.started = true
		o.acquired = true
	}
	if !o.suspended && transition.state != rendererOutputGateAltScreenUnchanged && o.acquired {
		if err := writeKittyKeyboardReporting(output, false); err != nil {
			return 0, err
		}
		o.acquired = false
	}

	n, err := output.Write(payload)
	if err != nil {
		return n, err
	}
	if n != len(payload) {
		return n, io.ErrShortWrite
	}
	o.screenParser = nextParser

	if !o.suspended && transition.state != rendererOutputGateAltScreenUnchanged {
		if err := writeKittyKeyboardReporting(output, true); err != nil {
			return n, err
		}
		o.acquired = true
	}
	return n, nil
}

func (o *uiTerminalKeyboardReportingOwner) Suspend() error {
	if o == nil || o.output == nil || !o.enabled {
		return nil
	}
	o.output.mu.Lock()
	defer o.output.mu.Unlock()
	if o.closed || o.suspended {
		return nil
	}
	if o.acquired {
		if err := writeKittyKeyboardReporting(o.output.out, false); err != nil {
			return err
		}
		o.acquired = false
	}
	o.suspended = true
	return nil
}

func (o *uiTerminalKeyboardReportingOwner) Resume() error {
	if o == nil || o.output == nil || !o.enabled {
		return nil
	}
	o.output.mu.Lock()
	defer o.output.mu.Unlock()
	if o.closed || !o.suspended {
		return nil
	}
	if o.started && !o.acquired {
		if err := writeKittyKeyboardReporting(o.output.out, true); err != nil {
			return err
		}
		o.acquired = true
	}
	o.suspended = false
	return nil
}

func (o *uiTerminalKeyboardReportingOwner) Close() error {
	if o == nil || o.output == nil || !o.enabled {
		return nil
	}
	o.output.mu.Lock()
	defer o.output.mu.Unlock()
	if o.closed {
		return nil
	}
	if o.acquired {
		if err := writeKittyKeyboardReporting(o.output.out, false); err != nil {
			return err
		}
		o.acquired = false
	}
	o.closed = true
	return nil
}

func writeKittyKeyboardReporting(output io.Writer, enable bool) error {
	sequence := xansi.PopKittyKeyboard(1)
	if enable {
		sequence = xansi.PushKittyKeyboard(xansi.KittyReportAllKeysAsEscapeCodes | xansi.KittyReportAssociatedKeys)
	}
	n, err := io.WriteString(output, sequence)
	if err != nil {
		return err
	}
	if n != len(sequence) {
		return fmt.Errorf("write terminal keyboard reporting: %w", io.ErrShortWrite)
	}
	return nil
}
