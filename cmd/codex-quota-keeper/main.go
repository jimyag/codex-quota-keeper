package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	appversion "github.com/jimmicro/version"
	"github.com/spf13/cobra"

	"github.com/jimyag/codex-quota-keeper/internal/codex"
	"github.com/jimyag/codex-quota-keeper/internal/config"
	"github.com/jimyag/codex-quota-keeper/internal/policy"
	keeperstate "github.com/jimyag/codex-quota-keeper/internal/state"
)

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
		Short:         "Redeem Codex reset credits before they expire",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       appversion.Version(),
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
		Short: "Continuously scan reset credits",
		Long:  "Scan immediately, then continue scanning at scan_interval until stopped.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runContinuously(runPath, runStatePath, apply, jsonOutput)
		},
	}
	addScanFlags(runCommand, &runPath, &runStatePath, &apply, &jsonOutput)

	var oncePath string
	var onceStatePath string
	var onceApply bool
	var onceJSON bool
	onceCommand := &cobra.Command{
		Use:   "once",
		Short: "Scan reset credits once",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := scanOnce(
				context.Background(),
				oncePath,
				onceStatePath,
				onceApply,
				onceJSON,
			)
			return err
		},
	}
	addScanFlags(onceCommand, &oncePath, &onceStatePath, &onceApply, &onceJSON)

	versionCommand := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(appversion.Version())
		},
	}

	root.AddCommand(
		initCommand,
		runCommand,
		onceCommand,
		versionCommand,
	)
	return root
}

func addScanFlags(
	command *cobra.Command,
	configPath, statePath *string,
	apply, jsonOutput *bool,
) {
	command.Flags().StringVar(configPath, "config", config.DefaultPath(), "configuration file")
	command.Flags().StringVar(statePath, "state", "", "override state file")
	command.Flags().BoolVar(apply, "apply", false, "redeem eligible reset credits")
	command.Flags().BoolVar(jsonOutput, "json", false, "print machine-readable scan results")
}

func initConfig(path string) error {
	written, err := config.WriteDefault(path)
	if err != nil {
		return err
	}
	fmt.Printf("created %s\n", written)
	fmt.Println("set reset_credit.enabled to true, then run codex-quota-keeper run --apply")
	return nil
}

func runContinuously(configPath, statePath string, apply, jsonOutput bool) error {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	runLoop(ctx, configPath, statePath, apply, jsonOutput)
	return nil
}

func runLoop(
	ctx context.Context,
	configPath, statePath string,
	apply, jsonOutput bool,
) {
	for {
		interval, err := scanOnce(ctx, configPath, statePath, apply, jsonOutput)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("scan failed: %v", err)
		}
		if interval <= 0 {
			interval = time.Minute
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func scanOnce(
	ctx context.Context,
	path, statePath string,
	apply, jsonOutput bool,
) (time.Duration, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return 0, err
	}
	if statePath != "" {
		expanded, err := config.ExpandPath(statePath)
		if err != nil {
			return cfg.ScanIntervalDuration, err
		}
		cfg.StateFile = expanded
	}

	now := time.Now()
	result := policy.Result{}
	var credits *codex.ResetCreditsSummary
	actionLog := make([]string, 0, 1)

	if !cfg.ResetCredit.Enabled {
		result.Messages = append(result.Messages, "reset credit: automatic redemption is disabled")
	} else {
		credits, err = listResetCredits(ctx, cfg)
		if err != nil {
			return cfg.ScanIntervalDuration, err
		}
		current, err := keeperstate.Load(cfg.StateFile)
		if err != nil {
			return cfg.ScanIntervalDuration, err
		}
		result = policy.Evaluate(now, cfg.ResetRedeemBeforeDuration, credits, &current)

		if result.CreditAction != nil {
			action := *result.CreditAction
			if !apply {
				actionLog = append(actionLog, fmt.Sprintf(
					"dry-run: would redeem reset credit %s before %s to avoid wasting it",
					action.CreditHash,
					action.ExpiresAt.Format(time.RFC3339),
				))
			} else {
				consumeResult, err := consumeResetCredit(ctx, cfg, action.Credit.ID)
				if err != nil {
					return cfg.ScanIntervalDuration, err
				}
				policy.MarkCreditRedeemed(&current, action, now)
				actionLog = append(actionLog, fmt.Sprintf(
					"redeemed expiring reset credit %s; windows_reset=%d",
					action.CreditHash,
					consumeResult.WindowsReset,
				))
			}
		}

		if err := current.Save(cfg.StateFile); err != nil {
			return cfg.ScanIntervalDuration, err
		}
	}

	if jsonOutput {
		err = printJSON(now, apply, credits, result, actionLog)
	} else {
		printHuman(now, apply, credits, result, actionLog)
	}
	return cfg.ScanIntervalDuration, err
}

func listResetCredits(
	parent context.Context,
	cfg config.Runtime,
) (*codex.ResetCreditsSummary, error) {
	ctx, cancel := context.WithTimeout(parent, cfg.ResetRequestTimeoutDuration)
	defer cancel()
	return codex.ListResetCredits(
		ctx,
		cfg.ResetCredit.AuthFile,
		cfg.ResetCredit.BaseURL,
		cfg.ResetRequestTimeoutDuration,
	)
}

func consumeResetCredit(
	parent context.Context,
	cfg config.Runtime,
	id string,
) (codex.ConsumeResult, error) {
	ctx, cancel := context.WithTimeout(parent, cfg.ResetRequestTimeoutDuration)
	defer cancel()
	return codex.ConsumeResetCredit(
		ctx,
		cfg.ResetCredit.AuthFile,
		cfg.ResetCredit.BaseURL,
		id,
		cfg.ResetRequestTimeoutDuration,
	)
}

func printHuman(
	now time.Time,
	apply bool,
	credits *codex.ResetCreditsSummary,
	result policy.Result,
	actionLog []string,
) {
	mode := "dry-run"
	if apply {
		mode = "apply"
	}
	fmt.Printf("time: %s\nmode: %s\n", now.Format(time.RFC3339), mode)
	if credits != nil {
		fmt.Printf("available reset credits: %d\n", credits.AvailableCount)
		if credits.Credits != nil {
			for _, credit := range *credits.Credits {
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
	credits *codex.ResetCreditsSummary,
	result policy.Result,
	actionLog []string,
) error {
	type safeCreditOutput struct {
		Hash      string `json:"hash"`
		Status    string `json:"status"`
		ExpiresAt *int64 `json:"expires_at,omitempty"`
	}
	type resetCreditOutput struct {
		AvailableCount int64              `json:"available_count"`
		Credits        []safeCreditOutput `json:"credits,omitempty"`
	}

	var safeCredits *resetCreditOutput
	if credits != nil {
		safeCredits = &resetCreditOutput{AvailableCount: credits.AvailableCount}
		if credits.Credits != nil {
			for _, credit := range *credits.Credits {
				safeCredits.Credits = append(safeCredits.Credits, safeCreditOutput{
					Hash:      policy.CreditHash(credit.ID),
					Status:    credit.Status,
					ExpiresAt: credit.ExpiresAt,
				})
			}
		}
	}
	output := struct {
		Time        time.Time          `json:"time"`
		Apply       bool               `json:"apply"`
		ResetCredit *resetCreditOutput `json:"reset_credit,omitempty"`
		Decision    policy.Result      `json:"decision"`
		Actions     []string           `json:"actions"`
	}{
		Time:        now,
		Apply:       apply,
		ResetCredit: safeCredits,
		Decision:    result,
		Actions:     actionLog,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
