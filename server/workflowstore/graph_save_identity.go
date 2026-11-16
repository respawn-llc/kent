package workflowstore

import (
	"fmt"

	"core/shared/runtimeids"
)

func validateWorkflowGraphSaveIdentities(req WorkflowGraphSaveRequest) error {
	check := func(value string, field string) error {
		_, err := runtimeids.ParseCanonicalUUIDv4(value, field)
		return err
	}
	for index, group := range req.NodeGroups {
		if err := check(group.ID, fmt.Sprintf("node_groups[%d].id", index)); err != nil {
			return err
		}
	}
	for index, node := range req.Nodes {
		if err := check(string(node.ID), fmt.Sprintf("nodes[%d].id", index)); err != nil {
			return err
		}
		if node.GroupID != nil {
			if err := check(*node.GroupID, fmt.Sprintf("nodes[%d].group_id", index)); err != nil {
				return err
			}
		}
		for providerIndex, provider := range node.JoinInputProviders {
			if err := check(string(provider.ProviderEdgeID), fmt.Sprintf("nodes[%d].join_input_providers[%d].provider_edge_id", index, providerIndex)); err != nil {
				return err
			}
		}
	}
	for index, group := range req.TransitionGroups {
		if err := check(string(group.ID), fmt.Sprintf("transition_groups[%d].id", index)); err != nil {
			return err
		}
		if err := check(string(group.SourceNodeID), fmt.Sprintf("transition_groups[%d].source_node_id", index)); err != nil {
			return err
		}
	}
	for index, edge := range req.Edges {
		if err := check(string(edge.ID), fmt.Sprintf("edges[%d].id", index)); err != nil {
			return err
		}
		if err := check(string(edge.TransitionGroupID), fmt.Sprintf("edges[%d].transition_group_id", index)); err != nil {
			return err
		}
		if err := check(string(edge.TargetNodeID), fmt.Sprintf("edges[%d].target_node_id", index)); err != nil {
			return err
		}
	}
	return nil
}
