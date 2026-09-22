package app

import (
	"context"
	"errors"
	"fmt"

	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"

	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *remoteAppServer) EnsureConnectionSetup(ctx context.Context) error {
	catalog, err := s.remote.GetConnections(ctx, &authpb.GetConnectionsRequest{})
	if err != nil {
		return err
	}
	if len(catalog.Connections) != 0 {
		return nil
	}
	return s.manageConnections(ctx, catalog)
}

func (s *remoteAppServer) manageConnections(ctx context.Context, catalog *authpb.ConnectionCatalog) error {
	selectedTheme := s.PresentationTheme()
	var workspace *string
	if s.cfg.WorkspaceRoot != "" {
		workspace = &s.cfg.WorkspaceRoot
	}
	var err error
	options := []startupPickerOption{{ID: "add", Title: "Add connection"}}
	existing := map[string]*authpb.ConnectionDefinition{}
	for index, definition := range catalog.Connections {
		key := fmt.Sprintf("existing-%d", index)
		options = append(options, startupPickerOption{ID: key, Title: definition.Id})
		existing[key] = definition
	}
	choice := "add"
	if len(catalog.Connections) > 0 {
		picked, err := runStartupPickerFlow(newStartupPickerModel("**Provider connections**", "Provider connections", selectedTheme, startupPickerNotice{}, options))
		if err != nil {
			return err
		}
		if picked.Canceled {
			return nil
		}
		choice = picked.ChoiceID
	}
	var selected *authpb.ConnectionDefinition
	if choice == "add" {
		form, model, err := newConnectionFormModel(selectedTheme, catalog, false)
		if err != nil {
			return err
		}
		err = runConnectionForm(ctx, model, func() error {
			selected = protoapi.ConnectionToProto(form.id, form.definition)
			if selected.Protocol == authpb.ConnectionProtocol_CONNECTION_PROTOCOL_CHATGPT {
				return signInConnection(ctx, s.remote, selectedTheme, &authpb.ConnectionTarget{Target: &authpb.ConnectionTarget_AddConnection{AddConnection: selected}}, false)
			}
			_, err = runConnectionOperation(ctx, selectedTheme, "Saving connection...", func() (*emptypb.Empty, error) {
				return s.remote.ConfigureConnection(ctx, &authpb.ConfigureConnectionRequest{Change: &authpb.ConfigureConnectionRequest_Add{Add: selected}})
			})
			return err
		})
		if err != nil {
			return err
		}
	} else {
		selected = existing[choice]
		if selected == nil {
			return errors.New("selected connection is not in the catalog")
		}
		id, definition, decodeErr := protoapi.ConnectionFromProto(selected)
		if decodeErr != nil {
			return decodeErr
		}
		switch connectionTemplateFor(definition) {
		case connectionTemplateSubscription:
			err = signInConnection(ctx, s.remote, selectedTheme, protoapi.ExistingConnectionTarget(id), true)
		case connectionTemplateAPI:
			err = editConnectionReference(ctx, s.remote, selectedTheme, id, definition)
		case connectionTemplateAnonymous:
			picked, infoErr := runStartupPickerFlow(newStartupPickerModel("**"+selected.Id+"**", selected.Id, selectedTheme,
				startupPickerNotice{Text: "This connection does not require sign-in.", Kind: startupPickerNoticeNeutral},
				[]startupPickerOption{{ID: "continue", Title: "Continue"}}))
			if infoErr != nil {
				return infoErr
			}
			if picked.Canceled {
				return nil
			}
		}
	}
	if err != nil {
		return err
	}
	if catalog.DefaultConnectionId != nil && *catalog.DefaultConnectionId == selected.Id {
		return nil
	}
	defaultPicker := newStartupPickerModel("**Default connection**", "Default connection", selectedTheme,
		startupPickerNotice{Text: "Changing the global default preserves role assignments and saved Session connections.", Kind: startupPickerNoticeNeutral},
		[]startupPickerOption{{ID: "keep", Title: "Keep the current default"}, {ID: "default", Title: "Make " + selected.Id + " the global default"}})
	if len(catalog.Connections) == 0 {
		defaultPicker.cursor = 1
	}
	picked, err := runStartupPickerFlow(defaultPicker)
	if err != nil {
		return err
	}
	if picked.Canceled || picked.ChoiceID == "keep" {
		return nil
	}
	if picked.ChoiceID != "default" {
		return errors.New("invalid default connection choice")
	}
	catalog, err = runConnectionOperation(ctx, selectedTheme, "Saving default connection...", func() (*authpb.ConnectionCatalog, error) {
		if _, err := s.remote.ConfigureConnection(ctx, &authpb.ConfigureConnectionRequest{Change: &authpb.ConfigureConnectionRequest_DefaultConnectionId{DefaultConnectionId: selected.Id}}); err != nil {
			return nil, err
		}
		catalog, err := s.remote.GetConnections(ctx, &authpb.GetConnectionsRequest{WorkspaceRoot: workspace})
		if err != nil {
			return nil, fmt.Errorf("Global default saved, but workspace configuration could not be read: %w", err)
		}
		return catalog, nil
	})
	if err != nil {
		return err
	}
	if catalog.WorkspaceConnectionId != nil {
		_, err = runStartupPickerFlow(newStartupPickerModel("**Global default saved**", "Global default saved", selectedTheme,
			startupPickerNotice{Text: "This workspace still overrides the default with " + *catalog.WorkspaceConnectionId + ".", Kind: startupPickerNoticeNeutral},
			[]startupPickerOption{{ID: "done", Title: "Done"}}))
	}
	return err
}

func editConnectionReference(ctx context.Context, remote apicontract.ConnectionManagementService, selectedTheme string, id config.ConnectionID, definition config.ProviderConnection) error {
	form := &connectionForm{template: connectionTemplateAPI, id: id, definition: definition}
	theme, err := seedThemeSelection(selectedTheme)
	if err != nil {
		return err
	}
	state := onboardingFlowState{selections: onboardingSelections{theme: theme}}
	steps := form.steps()
	model := newOnboardingFormModel(state, onboardingWorkflow{steps: steps[len(steps)-1:]})
	return runConnectionForm(ctx, model, func() error {
		_, err := runConnectionOperation(ctx, selectedTheme, "Saving environment reference...", func() (*emptypb.Empty, error) {
			return remote.ConfigureConnection(ctx, &authpb.ConfigureConnectionRequest{Change: &authpb.ConfigureConnectionRequest_Reference{
				Reference: &authpb.ConnectionReferenceEdit{ConnectionId: string(id), EnvironmentVariable: *form.definition.EnvironmentVariable},
			}})
		})
		return err
	})
}
