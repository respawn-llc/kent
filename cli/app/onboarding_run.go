package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	capabilitypb "core/shared/protoapi/gen/kent/api/capability"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

var ErrOnboardingCanceled = errors.New("first-time setup canceled")

type onboardingConnectionClient interface {
	apicontract.ConnectionManagementService
	apicontract.AuthBootstrapService
}

func runOnboardingFlow(ctx context.Context, cfg config.App, factsClient apicontract.CapabilityFactsService, finalizer apicontract.OnboardingFinalizeService, connections onboardingConnectionClient) (onboardingResult, error) {
	if err := ctx.Err(); err != nil {
		return onboardingResult{}, err
	}
	if factsClient == nil {
		return onboardingResult{}, errors.New("capability facts client is required")
	}
	if finalizer == nil {
		return onboardingResult{}, errors.New("onboarding finalization client is required")
	}
	if connections == nil {
		return onboardingResult{}, errors.New("connection setup client is required")
	}
	discard := func() (onboardingResult, error) {
		_, err := connections.ConfigureConnection(ctx, &authpb.ConfigureConnectionRequest{Change: &authpb.ConfigureConnectionRequest_DiscardSetup{DiscardSetup: &emptypb.Empty{}}})
		return onboardingResult{}, errors.Join(ErrOnboardingCanceled, presentConnectionError(err))
	}
	var workspaceRoot *string
	if strings.TrimSpace(cfg.WorkspaceRoot) != "" {
		workspaceRoot = &cfg.WorkspaceRoot
	}
	catalog, err := runConnectionOperation(ctx, string(cfg.Settings.Theme), "Loading connections...", func() (*authpb.ConnectionCatalog, error) {
		return connections.GetConnections(ctx, &authpb.GetConnectionsRequest{})
	})
	if errors.Is(err, ErrAuthCanceledByUser) {
		return discard()
	}
	if err != nil {
		return onboardingResult{}, err
	}
	form, formModel, err := newConnectionFormModel(string(cfg.Settings.Theme), catalog, true)
	if err != nil {
		return onboardingResult{}, err
	}
	finalization := newOnboardingFinalization(finalizer, ctx)
	var model *onboardingModel
	var factsFor *authpb.ConnectionDefinition
	for {
		formModel.state.pendingAction = onboardingPendingActionNone
		if _, err := runOnboardingProgram(ctx, formModel); err != nil {
			return onboardingResult{}, err
		}
		if formModel.canceled {
			return discard()
		}
		if formModel.terminalErr != nil {
			return onboardingResult{}, formModel.terminalErr
		}
		selectedTheme := formModel.state.selections.themeValue()
		definition := protoapi.ConnectionToProto(form.id, form.definition)
		_, err := runConnectionOperation(ctx, selectedTheme, "Preparing connection...", func() (*emptypb.Empty, error) {
			current, err := connections.GetConnections(ctx, &authpb.GetConnectionsRequest{})
			if err != nil {
				return nil, err
			}
			if !proto.Equal(current.PendingSetup, definition) {
				if _, err := connections.ConfigureConnection(ctx, &authpb.ConfigureConnectionRequest{Change: &authpb.ConfigureConnectionRequest_PendingSetup{PendingSetup: definition}}); err != nil {
					return nil, err
				}
			}
			return &emptypb.Empty{}, nil
		})
		if err == nil && form.definition.Protocol == config.ConnectionChatGPT {
			err = signInConnection(ctx, connections, selectedTheme, &authpb.ConnectionTarget{Target: &authpb.ConnectionTarget_PendingSetup{PendingSetup: &emptypb.Empty{}}}, false)
		}
		if errors.Is(err, ErrAuthCanceledByUser) {
			return discard()
		}
		if err != nil {
			formModel.errorText = connectionOperationErrorText(err)
			formModel.syncScreen(false)
			continue
		}
		if !proto.Equal(factsFor, definition) {
			facts, err := runConnectionOperation(ctx, selectedTheme, "Loading provider options...", func() (*capabilitypb.Facts, error) {
				return factsClient.GetFacts(ctx, &capabilitypb.GetFactsRequest{WorkspaceRoot: workspaceRoot})
			})
			if errors.Is(err, ErrAuthCanceledByUser) {
				return discard()
			}
			if err != nil {
				return onboardingResult{}, err
			}
			if model == nil {
				state, err := newOnboardingFlowState(cfg, facts)
				if err != nil {
					return onboardingResult{}, fmt.Errorf("initialize first-time setup selections: %w", err)
				}
				model = newOnboardingModel(finalization, state)
			} else {
				model.state.facts = facts
				model.state.imports = onboardingImportDiscoveryFromFacts(facts.GetImports())
				if err := model.state.submitPrimaryModel(model.state.selections.model.value); err != nil {
					return onboardingResult{}, err
				}
				if err := model.state.refreshSkillSelectionsAfterDiscovery(); err != nil {
					return onboardingResult{}, err
				}
			}
			factsFor = definition
		}
		model.state.selections.theme = formModel.state.selections.theme
		model.state.pendingAction = onboardingPendingActionNone
		model.stepIndex = 1
		model.syncScreen(true)
		finalModel, runErr := runOnboardingProgram(ctx, model)
		outcome, submitted := finalization.waitIfSubmitted()
		if runErr != nil {
			if submitted {
				return onboardingFlowOutcome(outcome)
			}
			return onboardingResult{}, runErr
		}
		finalized, ok := finalModel.(*onboardingModel)
		if !ok {
			if submitted {
				return onboardingFlowOutcome(outcome)
			}
			return onboardingResult{}, fmt.Errorf("unexpected onboarding model type %T", finalModel)
		}
		if finalized.terminalErr != nil {
			return onboardingResult{}, finalized.terminalErr
		}
		if submitted {
			return onboardingFlowOutcome(outcome)
		}
		if finalized.canceled {
			return discard()
		}
		if finalized.state.pendingAction == onboardingPendingActionRestart {
			formModel.stepIndex = 0
			formModel.syncScreen(true)
			continue
		}
		if finalized.state.pendingAction == onboardingPendingActionConnectionSetup {
			continue
		}
		return finalized.result, nil
	}
}

func runOnboardingProgram(ctx context.Context, model *onboardingModel) (tea.Model, error) {
	cursor := newUITerminalCursorState()
	model.terminalCursor = cursor
	return runStartupAlternateScreen(ctx, model, newUITerminalCursorWriter(os.Stdout, cursor))
}

func onboardingFlowOutcome(outcome onboardingFinalizeDoneMsg) (onboardingResult, error) {
	if outcome.err != nil {
		return onboardingResult{}, outcome.err
	}
	return outcome.result, nil
}
