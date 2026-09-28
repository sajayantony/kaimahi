package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	agentruntime "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
)

func (a orkaRuntimeAdapter) Logs(ctx context.Context, ref agentruntime.ExecutionRef, options agentruntime.LogOptions, emit agentruntime.EmitLog) error {
	if err := a.lifecycleVerbError(a.Capabilities().Logs, agentruntime.VerbLogs); err != nil {
		return err
	}
	if ref.Runtime != a.ID() || strings.TrimSpace(ref.Namespace) == "" ||
		strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(ref.UID) == "" {
		return fmt.Errorf("invalid execution reference for runtime %s", a.ID())
	}
	selector := "orka.ai/task=" + ref.Name
	for {
		raw, err := a.app.orkaCapture(ctx, nil, "-n", ref.Namespace, "get", "pods",
			"-l", selector, "-o", "name")
		if err != nil {
			return fmt.Errorf("find logs for execution %s: %w", ref.Name, err)
		}
		if strings.TrimSpace(string(raw)) != "" {
			break
		}
		if err := orkaPause(ctx); err != nil {
			return fmt.Errorf("wait for logs for execution %s: %w", ref.Name, err)
		}
	}

	args := []string{"-n", ref.Namespace, "logs", "-l", selector,
		"--all-containers=true", "--prefix=true", fmt.Sprintf("--tail=%d", options.Tail),
		"--max-log-requests=1"}
	if options.Follow {
		args = append(args, "-f")
	}
	prepared := a.app.Command(args...)
	cmd := exec.CommandContext(ctx, prepared.Path, prepared.Args[1:]...)
	cmd.Env = prepared.Env
	cmd.WaitDelay = time.Second
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open logs for execution %s", ref.Name)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start logs for execution %s", ref.Name)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		if err := emit(agentruntime.LogEntry{Source: ref.Name, Message: scanner.Text()}); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("read logs for execution %s", ref.Name)
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("follow logs for execution %s failed", ref.Name)
	}
	return nil
}
