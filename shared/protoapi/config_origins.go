package protoapi

import (
	configurationpb "core/shared/protoapi/gen/kent/api/configuration"
	"fmt"

	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/toolspec"
	"google.golang.org/protobuf/types/known/emptypb"
)

func configOriginToProto(origin config.Origin) (*configurationpb.ConfigOrigin, error) {
	message := &configurationpb.ConfigOrigin{Property: &configurationpb.ConfigPropertyAddress{
		Key: origin.Property.Key, Role: origin.Property.Role,
	}}
	if origin.RetainedSessionID != nil {
		id := origin.RetainedSessionID.String()
		message.RetainedSessionId = &id
	}
	switch origin.Kind {
	case config.SourceDefault:
		message.Source = &configurationpb.ConfigOrigin_DefaultValue{DefaultValue: &emptypb.Empty{}}
	case config.SourceFileKind:
		if origin.File == nil {
			return nil, fmt.Errorf("configuration file origin requires its file")
		}
		layer, err := configFileLayerToProto(origin.File.Layer)
		if err != nil {
			return nil, err
		}
		message.Source = &configurationpb.ConfigOrigin_File{File: &configurationpb.ConfigFileSource{Layer: layer, Path: origin.File.Path}}
	case config.SourceEnv:
		if origin.Option == nil {
			return nil, fmt.Errorf("configuration environment origin requires its variable")
		}
		message.Source = &configurationpb.ConfigOrigin_Environment{Environment: *origin.Option}
	case config.SourceCLI:
		if origin.Option == nil {
			return nil, fmt.Errorf("configuration CLI origin requires its option")
		}
		message.Source = &configurationpb.ConfigOrigin_CliOption{CliOption: *origin.Option}
	case config.SourceInput:
		message.Source = &configurationpb.ConfigOrigin_Input{Input: &emptypb.Empty{}}
	case config.SourceSession:
		message.Source = &configurationpb.ConfigOrigin_Session{Session: &emptypb.Empty{}}
	default:
		return nil, fmt.Errorf("unknown configuration origin kind %q", origin.Kind)
	}
	return message, Validate(message)
}

func configOriginFromProto(message *configurationpb.ConfigOrigin) (config.Origin, error) {
	if err := Validate(message); err != nil {
		return config.Origin{}, err
	}
	origin := config.Origin{Property: config.PropertyAddress{Key: message.Property.Key, Role: message.Property.Role}}
	if message.RetainedSessionId != nil {
		id, err := runtimeids.ParseSessionID(*message.RetainedSessionId)
		if err != nil {
			return config.Origin{}, err
		}
		origin.RetainedSessionID = &id
	}
	switch source := message.Source.(type) {
	case *configurationpb.ConfigOrigin_DefaultValue:
		origin.Kind = config.SourceDefault
	case *configurationpb.ConfigOrigin_File:
		layer, err := configFileLayerFromProto(source.File.Layer)
		if err != nil {
			return config.Origin{}, err
		}
		origin.Kind = config.SourceFileKind
		origin.File = &config.SourceFile{Layer: layer, Path: source.File.Path}
	case *configurationpb.ConfigOrigin_Environment:
		origin.Kind, origin.Option = config.SourceEnv, &source.Environment
	case *configurationpb.ConfigOrigin_CliOption:
		origin.Kind, origin.Option = config.SourceCLI, &source.CliOption
	case *configurationpb.ConfigOrigin_Input:
		origin.Kind = config.SourceInput
	case *configurationpb.ConfigOrigin_Session:
		origin.Kind = config.SourceSession
	default:
		return config.Origin{}, fmt.Errorf("unknown configuration origin %T", message.Source)
	}
	return origin, nil
}

func ToolSelectionToProto(selection *config.ToolSelection) (*configurationpb.ToolSelection, error) {
	if selection == nil {
		return nil, nil
	}
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	origin, err := configOriginToProto(selection.Origin)
	if err != nil {
		return nil, err
	}
	message := &configurationpb.ToolSelection{Origin: origin}
	for _, id := range selection.Tools {
		tool, err := SessionToolIDToProto(id)
		if err != nil {
			return nil, err
		}
		message.Tools = append(message.Tools, tool)
	}
	return message, Validate(message)
}

func ToolSelectionFromProto(message *configurationpb.ToolSelection) (*config.ToolSelection, error) {
	if message == nil {
		return nil, nil
	}
	if err := Validate(message); err != nil {
		return nil, err
	}
	origin, err := configOriginFromProto(message.Origin)
	if err != nil {
		return nil, err
	}
	selection := &config.ToolSelection{Origin: origin, Tools: make([]toolspec.ID, 0, len(message.Tools))}
	for _, id := range message.Tools {
		tool, err := SessionToolIDFromProto(id)
		if err != nil {
			return nil, err
		}
		selection.Tools = append(selection.Tools, tool)
	}
	return selection, selection.Validate()
}

func configFileLayerToProto(layer config.FileLayer) (configurationpb.ConfigFileLayer, error) {
	switch layer {
	case config.FileGlobal:
		return configurationpb.ConfigFileLayer_CONFIG_FILE_LAYER_GLOBAL, nil
	case config.FileWorkspace:
		return configurationpb.ConfigFileLayer_CONFIG_FILE_LAYER_WORKSPACE, nil
	case config.FilePrivate:
		return configurationpb.ConfigFileLayer_CONFIG_FILE_LAYER_PRIVATE, nil
	default:
		return 0, fmt.Errorf("unknown configuration file layer %q", layer)
	}
}

func configFileLayerFromProto(layer configurationpb.ConfigFileLayer) (config.FileLayer, error) {
	switch layer {
	case configurationpb.ConfigFileLayer_CONFIG_FILE_LAYER_GLOBAL:
		return config.FileGlobal, nil
	case configurationpb.ConfigFileLayer_CONFIG_FILE_LAYER_WORKSPACE:
		return config.FileWorkspace, nil
	case configurationpb.ConfigFileLayer_CONFIG_FILE_LAYER_PRIVATE:
		return config.FilePrivate, nil
	default:
		return "", fmt.Errorf("unknown configuration file layer %v", layer)
	}
}

func sourceFactsToProto(sources map[string]config.Origin) ([]*configurationpb.SourceFact, error) {
	result := make([]*configurationpb.SourceFact, 0, len(sources))
	for _, key := range sortedStringKeys(sources) {
		origin, err := configOriginToProto(sources[key])
		if err != nil {
			return nil, fmt.Errorf("configuration source %q: %w", key, err)
		}
		result = append(result, &configurationpb.SourceFact{Key: key, Origin: origin})
	}
	return result, nil
}

func sourceFactsFromProto(facts []*configurationpb.SourceFact) (map[string]config.Origin, error) {
	result := make(map[string]config.Origin, len(facts))
	for _, fact := range facts {
		if err := Validate(fact); err != nil {
			return nil, err
		}
		if _, exists := result[fact.Key]; exists {
			return nil, fmt.Errorf("duplicate configuration source %q", fact.Key)
		}
		origin, err := configOriginFromProto(fact.Origin)
		if err != nil {
			return nil, err
		}
		result[fact.Key] = origin
	}
	return result, nil
}
