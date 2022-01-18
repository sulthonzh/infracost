package main

import (
	"context"
	"fmt"
	"os"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/spf13/cobra"

	"github.com/infracost/infracost/internal/config"
	"github.com/infracost/infracost/internal/output"
	"github.com/infracost/infracost/internal/ui"
)

func commentCmd(ctx *config.RunContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment",
		Short: "Post an Infracost comment to GitHub, GitLab or Azure Repos",
		Long:  "Post an Infracost comment to GitHub, GitLab or Azure Repos",
		Example: `  Update the Infracost comment on a GitHub pull request:

      infracost comment github --repo my-org/my-repo --pull-request 3 --path infracost.json --behavior update --github-token $GITHUB_TOKEN

  Delete old Infracost comments and post a new comment to a GitLab commit:

      infracost comment gitlab --repo my-org/my-repo --commit 2ca7182 --path infracost.json --behavior delete-and-new --gitlab-token $GITLAB_TOKEN

  Post a new comment to an Azure Repos pull request:

      infracost comment azure-repos --repo-url https://dev.azure.com/my-org/my-project/_git/my-repo --pull-request 3 --path infracost.json --behavior new --azure-access-token $AZURE_ACCESS_TOKEN`,
		ValidArgs: []string{"--", "-"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmds := []*cobra.Command{commentGitHubCmd(ctx), commentGitLabCmd(ctx), commentAzureReposCmd(ctx)}
	for _, subCmd := range cmds {
		subCmd.Flags().StringSlice("policy-path", nil, "Paths to any Infracost cost policies (experimental)")
		subCmd.Flags().Bool("comment-on-failure", false, "Only post a PR comment if a cost policy fails, must be used with --policy-path (experimental)")
	}

	cmd.AddCommand(cmds...)

	return cmd
}

func buildCommentBody(cmd *cobra.Command, ctx *config.RunContext, paths []string, mdOpts output.MarkdownOptions) ([]byte, error) {
	inputs, err := output.LoadPaths(paths)
	if err != nil {
		return nil, err
	}

	combined, err := output.Combine(inputs)
	if err != nil {
		return nil, err
	}
	combined.IsCIRun = ctx.IsCIRun()

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if ctx.Config.EnableDashboard && !dryRun {
		if ctx.Config.IsSelfHosted() {
			ui.PrintWarning(cmd.ErrOrStderr(), "The dashboard is part of Infracost's hosted services. Contact hello@infracost.io for help.")
		}

		combined.RunID, combined.ShareURL = shareCombinedRun(ctx, combined, inputs)
	}

	var policyChecks output.PolicyCheck
	policyPaths, _ := cmd.Flags().GetStringSlice("policy-path")
	if len(policyPaths) > 0 {
		policyChecks, err = queryPolicy(policyPaths, combined)
		if err != nil {
			return nil, err
		}
	}

	opts := output.Options{
		DashboardEnabled: ctx.Config.EnableDashboard,
		NoColor:          ctx.Config.NoColor,
		IncludeHTML:      true,
		ShowSkipped:      true,
		PolicyChecks:     policyChecks,
	}

	b, err := output.ToMarkdown(combined, opts, mdOpts)
	if err != nil {
		return nil, err
	}

	if policyChecks.HasFailed() {
		return b, policyChecks.Failures
	}

	return b, nil
}

func queryPolicy(policyPaths []string, input output.Root) (output.PolicyCheck, error) {
	checks := output.PolicyCheck{
		Enabled: true,
	}

	inputValue, err := ast.InterfaceToValue(input)
	if err != nil {
		return checks, fmt.Errorf("Unable to process infracost output into rego input: %s", err.Error())
	}

	ctx := context.Background()
	r := rego.New(
		rego.Query("data.infracost.deny"),
		rego.ParsedInput(inputValue),
		rego.Load(policyPaths, func(abspath string, info os.FileInfo, depth int) bool {
			return false
		}),
	)
	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return checks, fmt.Errorf("Unable to query cost policy: %s", err.Error())
	}

	res, err := pq.Eval(ctx)
	if err != nil {
		return checks, err
	}

	var errs []string
	for _, e := range res[0].Expressions {
		switch v := e.Value.(type) {
		case []interface{}:
			for _, i := range v {
				errs = append(errs, fmt.Sprintf("%s", i))
			}
		case interface{}:
			errs = append(errs, e.String())
		}
	}

	if len(errs) == 0 {
		return checks, nil
	}

	checks.Failures = errs
	return checks, nil
}
