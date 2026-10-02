package main

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
)

func newSuiteCommand(state *commandState) *cobra.Command {
	group := &cobra.Command{
		Use:   "suite",
		Short: "Inspect, package, and run an incubating Agent Suite",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	group.AddCommand(
		newSuiteInspectCommand(state),
		newSuitePackageCommand(state),
		newSuiteRunCommand(state),
	)
	return group
}

func newSuiteInspectCommand(state *commandState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect <suite-dir-or-oci-layout>",
		Short: "Inspect source or packaged identities, aggregation, and the Substrate lift plan",
		Args:  usageArgs(1, 1, "kmx suite inspect <suite-dir-or-oci-layout>"),
	}
	cmd.RunE = appRun(state, func(a *app.App) error {
		return a.InspectAgentSuite(cmd.Flags().Arg(0))
	})
	return cmd
}

func newSuitePackageCommand(state *commandState) *cobra.Command {
	var opt app.PackageAgentSuiteOptions
	cmd := &cobra.Command{
		Use:   "package <suite-dir>",
		Short: "Export a resolved suite as an OCI Image Layout artifact",
		Args:  usageArgs(1, 1, "kmx suite package <suite-dir> --output <oci-layout-dir>"),
	}
	cmd.Flags().StringVarP(&opt.Output, "output", "o", "", "empty directory to write as an OCI Image Layout (required)")
	_ = cmd.MarkFlagRequired("output")
	cmd.RunE = appRun(state, func(a *app.App) error {
		return a.PackageAgentSuite(cmd.Flags().Arg(0), opt)
	})
	return cmd
}

func newSuiteRunCommand(state *commandState) *cobra.Command {
	var opt app.RunAgentSuiteOptions
	cmd := &cobra.Command{
		Use:   "run <suite-dir>",
		Short: "Run specialists and aggregate their durable AgentSessions outputs",
		Args:  usageArgs(1, 1, "kmx suite run <suite-dir> --server <address> (--prompt <text>|--prompt-file <path>)"),
	}
	cmd.Flags().StringVar(&opt.AgentSessionsServer, "server", "", "AgentSessions gRPC server (required)")
	cmd.Flags().StringVar(&opt.AgentSessionsProject, "project", "default", "AgentSessions project")
	cmd.Flags().StringVar(&opt.Prompt, "prompt", "", "suite request")
	cmd.Flags().StringVar(&opt.PromptFile, "prompt-file", "", "read suite request from a file, or - for stdin")
	cmd.Flags().DurationVar(&opt.Wait, "wait", 5*time.Minute, "maximum duration for the complete suite run")
	_ = cmd.MarkFlagRequired("server")
	cmd.RunE = appRun(state, func(a *app.App) error {
		return a.RunAgentSuite(cmd.Flags().Arg(0), opt)
	})
	return cmd
}
