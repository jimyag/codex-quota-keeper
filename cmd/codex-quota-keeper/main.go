package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jimyag/codex-quota-keeper/internal/codex"
	"github.com/jimyag/codex-quota-keeper/internal/config"
	"github.com/jimyag/codex-quota-keeper/internal/notify"
	"github.com/jimyag/codex-quota-keeper/internal/policy"
	"github.com/jimyag/codex-quota-keeper/internal/runner"
	keeperstate "github.com/jimyag/codex-quota-keeper/internal/state"
)

var version = "dev"

func main() {
	log.SetFlags(log.LstdFlags)
	if err := newRootCommand().Execute(); err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "codex-quota-keeper",
		Short:         "Manage Codex rolling windows and expiring reset credits",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       version,
	}

	var initPath string
	initCommand := &cobra.Command{
		Use:   "init",
		Short: "Create a default configuration file",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return initConfig(initPath)
		},
	}
	initCommand.Flags().StringVar(&initPath, "config", config.DefaultPath(), "configuration file")

	var runPath string
	var runStatePath string
	var apply bool
	var jsonOutput bool
	runCommand := &cobra.Command{
		Use:   "run",
		Short: "Evaluate quota policies once",
		Long:  "Evaluate quota policies once. The command is a dry-run unless --apply is provided.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runOnce(runPath, runStatePath, apply, jsonOutput)
		},
	}
	runCommand.Flags().StringVar(&runPath, "config", config.DefaultPath(), "configuration file")
	runCommand.Flags().StringVar(&runStatePath, "state", "", "override state file")
	runCommand.Flags().BoolVar(&apply, "apply", false, "perform eligible actions")
	runCommand.Flags().BoolVar(&jsonOutput, "json", false, "print a machine-readable result")

	var cancelPath string
	var cancelStatePath string
	cancelCommand := &cobra.Command{
		Use:   "cancel",
		Short: "Skip all currently pending reset credits",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return cancelPending(cancelPath, cancelStatePath)
		},
	}
	cancelCommand.Flags().StringVar(&cancelPath, "config", config.DefaultPath(), "configuration file")
	cancelCommand.Flags().StringVar(&cancelStatePath, "state", "", "override state file")

	versionCommand := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(version)
		},
	}

	root.AddCommand(initCommand, runCommand, cancelCommand, versionCommand)
	return root
}

func initConfig(path string) error {
	written, err := config.WriteDefault(path)
	if err != nil {
		return err
	}
	fmt.Printf("created %s\n", written)
	fmt.Println("both automatic actions are disabled by default")
	return nil
}

func runOnce(path, statePath string, apply, jsonOutput bool) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if statePath != "" {
		expanded, err := config.ExpandPath(statePath)
		if err != nil {
			return err
		}
		cfg.StateFile = expanded
	}
	current, err := keeperstate.Load(cfg.StateFile)
	if err != nil {
		return err
	}

	fetchCtx, cancelFetch := context.WithTimeout(context.Background(), cfg.FetchTimeoutDuration)
	limits, err := codex.ReadRateLimits(fetchCtx, cfg.CodexBinary)
	cancelFetch()
	if err != nil {
		return err
	}
	if cfg.ResetCredit.Enabled &&
		(limits.RateLimitResetCredits == nil || limits.RateLimitResetCredits.Credits == nil) {
		creditCtx, cancelCredits := context.WithTimeout(
			context.Background(),
			cfg.ResetRequestTimeoutDuration,
		)
		details, err := codex.ListResetCredits(
			creditCtx,
			cfg.ResetCredit.AuthFile,
			cfg.ResetCredit.BaseURL,
			cfg.ResetRequestTimeoutDuration,
		)
		cancelCredits()
		if err != nil {
			return err
		}
		limits.RateLimitResetCredits = details
	}

	now := time.Now()
	result := policy.Evaluate(now, cfg, limits, &current)
	actionLog := make([]string, 0, 2)

	if result.CreditPending != nil {
		pending := result.CreditPending
		_ = notify.Send(
			"Codex reset credit pending",
			fmt.Sprintf(
				"Credit %s becomes eligible at %s. Run codex-quota-keeper cancel to skip it.",
				pending.CreditHash,
				pending.ReadyAt.Format(time.Kitchen),
			),
		)
	}

	if result.WindowAction != nil {
		if !apply {
			actionLog = append(actionLog, "dry-run: would open a new Codex primary window")
		} else {
			if err := runner.Run(context.Background(), cfg.Window.Command, cfg.WindowTimeoutDuration); err != nil {
				return err
			}
			policy.MarkWindowStarted(&current, *result.WindowAction, now)
			actionLog = append(actionLog, "opened a new Codex primary window")
			_ = notify.Send("Codex Quota Keeper", "Opened a new Codex usage window.")
		}
	}

	if result.CreditAction != nil {
		action := *result.CreditAction
		if !apply {
			actionLog = append(actionLog, fmt.Sprintf(
				"dry-run: would redeem reset credit %s before %s",
				action.CreditHash,
				action.ExpiresAt.Format(time.RFC3339),
			))
		} else {
			consumeCtx, cancelConsume := context.WithTimeout(
				context.Background(),
				cfg.ResetRequestTimeoutDuration,
			)
			consumeResult, err := codex.ConsumeResetCredit(
				consumeCtx,
				cfg.ResetCredit.AuthFile,
				cfg.ResetCredit.BaseURL,
				action.Credit.ID,
				cfg.ResetRequestTimeoutDuration,
			)
			cancelConsume()
			if err != nil {
				return err
			}
			policy.MarkCreditRedeemed(&current, action, now)
			actionLog = append(actionLog, fmt.Sprintf(
				"redeemed reset credit %s; windows_reset=%d",
				action.CreditHash,
				consumeResult.WindowsReset,
			))
			_ = notify.Send("Codex Quota Keeper", "Redeemed an expiring Codex reset credit.")
		}
	}

	if err := current.Save(cfg.StateFile); err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(now, apply, limits, result, actionLog)
	}
	printHuman(now, apply, limits, result, actionLog)
	return nil
}

func cancelPending(path, statePath string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if statePath != "" {
		expanded, err := config.ExpandPath(statePath)
		if err != nil {
			return err
		}
		cfg.StateFile = expanded
	}
	current, err := keeperstate.Load(cfg.StateFile)
	if err != nil {
		return err
	}
	count := len(current.PendingCredits)
	now := time.Now()
	for hash := range current.PendingCredits {
		current.SkippedCredits[hash] = now
	}
	current.PendingCredits = make(map[string]keeperstate.Pending)
	if err := current.Save(cfg.StateFile); err != nil {
		return err
	}
	fmt.Printf("cancelled and skipped %d pending reset credit action(s)\n", count)
	return nil
}

func printHuman(
	now time.Time,
	apply bool,
	limits codex.RateLimitsResponse,
	result policy.Result,
	actionLog []string,
) {
	mode := "dry-run"
	if apply {
		mode = "apply"
	}
	fmt.Printf("time: %s\nmode: %s\n", now.Format(time.RFC3339), mode)
	if limits.RateLimits.Primary != nil {
		fmt.Printf("primary used: %d%%\n", limits.RateLimits.Primary.UsedPercent)
	}
	if limits.RateLimits.Secondary != nil {
		fmt.Printf("secondary used: %d%%\n", limits.RateLimits.Secondary.UsedPercent)
	}
	if limits.RateLimitResetCredits != nil {
		fmt.Printf("available reset credits: %d\n", limits.RateLimitResetCredits.AvailableCount)
		if limits.RateLimitResetCredits.Credits != nil {
			for _, credit := range *limits.RateLimitResetCredits.Credits {
				expiry := "none"
				if credit.ExpiresAt != nil {
					expiry = time.Unix(*credit.ExpiresAt, 0).Format(time.RFC3339)
				}
				fmt.Printf(
					"reset credit: %s status=%s expires=%s\n",
					policy.CreditHash(credit.ID),
					credit.Status,
					expiry,
				)
			}
		}
	}
	for _, message := range result.Messages {
		fmt.Printf("info: %s\n", message)
	}
	for _, message := range actionLog {
		fmt.Printf("action: %s\n", message)
	}
	if len(actionLog) == 0 {
		fmt.Println("action: none")
	}
}

func printJSON(
	now time.Time,
	apply bool,
	limits codex.RateLimitsResponse,
	result policy.Result,
	actionLog []string,
) error {
	type windowOutput struct {
		UsedPercent int    `json:"used_percent"`
		ResetsAt    *int64 `json:"resets_at,omitempty"`
	}
	type safeCreditOutput struct {
		Hash      string `json:"hash"`
		Status    string `json:"status"`
		ExpiresAt *int64 `json:"expires_at,omitempty"`
	}
	type resetCreditOutput struct {
		AvailableCount int64              `json:"available_count"`
		Credits        []safeCreditOutput `json:"credits,omitempty"`
	}
	type limitsOutput struct {
		Primary     *windowOutput      `json:"primary,omitempty"`
		Secondary   *windowOutput      `json:"secondary,omitempty"`
		ResetCredit *resetCreditOutput `json:"reset_credit,omitempty"`
	}
	safeLimits := limitsOutput{}
	if limits.RateLimits.Primary != nil {
		safeLimits.Primary = &windowOutput{
			UsedPercent: limits.RateLimits.Primary.UsedPercent,
			ResetsAt:    limits.RateLimits.Primary.ResetsAt,
		}
	}
	if limits.RateLimits.Secondary != nil {
		safeLimits.Secondary = &windowOutput{
			UsedPercent: limits.RateLimits.Secondary.UsedPercent,
			ResetsAt:    limits.RateLimits.Secondary.ResetsAt,
		}
	}
	if limits.RateLimitResetCredits != nil {
		safeLimits.ResetCredit = &resetCreditOutput{
			AvailableCount: limits.RateLimitResetCredits.AvailableCount,
		}
		if limits.RateLimitResetCredits.Credits != nil {
			for _, credit := range *limits.RateLimitResetCredits.Credits {
				safeLimits.ResetCredit.Credits = append(
					safeLimits.ResetCredit.Credits,
					safeCreditOutput{
						Hash:      policy.CreditHash(credit.ID),
						Status:    credit.Status,
						ExpiresAt: credit.ExpiresAt,
					},
				)
			}
		}
	}
	output := struct {
		Time     time.Time     `json:"time"`
		Apply    bool          `json:"apply"`
		Limits   limitsOutput  `json:"limits"`
		Decision policy.Result `json:"decision"`
		Actions  []string      `json:"actions"`
	}{
		Time:     now,
		Apply:    apply,
		Limits:   safeLimits,
		Decision: result,
		Actions:  actionLog,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
