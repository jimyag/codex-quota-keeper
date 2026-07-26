package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

func Run(ctx context.Context, command []string, timeout time.Duration) error {
	if len(command) == 0 {
		return fmt.Errorf("command must not be empty")
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, command[0], command[1:]...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		if runCtx.Err() != nil {
			return runCtx.Err()
		}
		return fmt.Errorf("%s failed: %w: %s", command[0], err, output.String())
	}
	return nil
}
