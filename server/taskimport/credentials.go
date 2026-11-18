package taskimport

import (
	"context"

	"core/shared/githubcredentials"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
)

func selectCredential(cliEnv, serverEnv, cliLogin, serverLogin *string) *string {
	for _, token := range []*string{cliEnv, serverEnv, cliLogin, serverLogin} {
		if token != nil {
			return token
		}
	}
	return nil
}

func acquireCredential(ctx context.Context, cli *taskimportpb.CredentialCandidates, lookup func(string) (string, bool)) (*string, []string) {
	var warnings []string
	if cli == nil {
		cli = &taskimportpb.CredentialCandidates{}
	}
	if cli.LoginDiagnostic != nil {
		warnings = append(warnings, *cli.LoginDiagnostic)
	}
	token := selectCredential(cli.EnvironmentToken, githubcredentials.EnvironmentToken(lookup), cli.LoginToken, nil)
	if token != nil {
		return token, warnings
	}
	token, err := githubcredentials.LoginToken(ctx)
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	return token, warnings
}
