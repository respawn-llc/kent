package app

import (
	"context"
	"errors"
	"strings"

	"builder/shared/client"
	"builder/shared/clientui"
	"builder/shared/serverapi"
	"github.com/google/uuid"
)

type backgroundUIProcessClient struct {
	reads   client.ProcessViewClient
	control client.ProcessControlClient
}

func newUIProcessClientWithReads(reads client.ProcessViewClient, control client.ProcessControlClient) clientui.ProcessClient {
	if reads == nil && control == nil {
		return nil
	}
	return backgroundUIProcessClient{reads: reads, control: control}
}

func (m *uiModel) listProcesses() []clientui.BackgroundProcess {
	if m == nil || m.processClient == nil {
		return nil
	}
	return m.processClient.ListProcesses()
}

func (c backgroundUIProcessClient) ListProcesses() []clientui.BackgroundProcess {
	if c.reads != nil {
		resp, err := c.reads.ListProcesses(context.Background(), serverapi.ProcessListRequest{})
		if err != nil {
			return nil
		}
		return resp.Processes
	}
	return nil
}

func (c backgroundUIProcessClient) KillProcess(id string) error {
	id = strings.TrimSpace(id)
	if c.control != nil {
		_, err := c.control.KillProcess(context.Background(), serverapi.ProcessKillRequest{ClientRequestID: uuid.NewString(), ProcessID: id})
		if err != nil {
			return err
		}
		return nil
	}
	return errors.New("process control client is unavailable")
}

func (c backgroundUIProcessClient) InlineOutput(id string, maxChars int) (string, string, error) {
	id = strings.TrimSpace(id)
	if c.control != nil {
		resp, err := c.control.GetInlineOutput(context.Background(), serverapi.ProcessInlineOutputRequest{ProcessID: id, MaxChars: maxChars})
		if err != nil {
			return "", "", err
		}
		return resp.Output, resp.LogPath, nil
	}
	return "", "", errors.New("process control client is unavailable")
}
