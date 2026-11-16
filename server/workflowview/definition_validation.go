package workflowview

import (
	"core/server/workflow"
	"core/server/workflowscript"
)

func definitionExecutionValidation(def workflow.Definition, roleResolver workflow.RoleResolver) workflow.ValidationResult {
	return workflowscript.EvaluateDefinition(
		def,
		[]workflow.ValidationContext{workflow.ValidationContextExecution},
		roleResolver,
		nil,
	)[workflow.ValidationContextExecution]
}
