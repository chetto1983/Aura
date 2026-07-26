package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

const adaptiveBenchmarkRunUsage = "usage: aura eval adaptive benchmark " +
	"--mode {qwen-portability|production-shadow} --owner-id <uuid> " +
	"--output-dir <absolute-path> [--confirm-temporary-settings-override]"

type adaptiveBenchmarkRunRequest struct {
	OwnerID                   uuid.UUID
	OutputDir                 string
	Mode                      auraeval.AdaptiveBenchmarkMode
	ConfirmedSettingsOverride bool
}

type adaptiveBenchmarkRunExecution interface {
	Run(context.Context) error
	Close() error
	Finalize(context.Context) ([]byte, error)
}

type adaptiveBenchmarkRunFactory func(
	context.Context,
	adaptiveBenchmarkRunRequest,
) (adaptiveBenchmarkRunExecution, error)

type adaptiveBenchmarkSignalNotifier func(
	context.Context,
	...os.Signal,
) (context.Context, context.CancelFunc)

func runAdaptiveBenchmarkRunCommand(
	ctx context.Context,
	args []string,
	output io.Writer,
	factory adaptiveBenchmarkRunFactory,
) error {
	return runAdaptiveBenchmarkRunCommandWithSignalNotifier(
		ctx,
		args,
		output,
		factory,
		signal.NotifyContext,
	)
}

func runAdaptiveBenchmarkRunCommandWithSignalNotifier(
	ctx context.Context,
	args []string,
	output io.Writer,
	factory adaptiveBenchmarkRunFactory,
	notify adaptiveBenchmarkSignalNotifier,
) (returnedErr error) {
	request, err := parseAdaptiveBenchmarkRunArgs(args)
	if err != nil {
		return err
	}
	if output == nil || factory == nil || notify == nil {
		return errors.New("adaptive benchmark command is unavailable")
	}
	signalCtx, stop := notify(
		ctx,
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	execution, err := factory(signalCtx, request)
	if err != nil {
		return err
	}
	if execution == nil {
		return errors.New("adaptive benchmark execution is unavailable")
	}
	executionOwned := true
	defer func() {
		if executionOwned {
			returnedErr = errors.Join(returnedErr, execution.Close())
		}
	}()
	runErr := execution.Run(signalCtx)
	executionOwned = false
	closeErr := execution.Close()
	if runErr != nil || closeErr != nil {
		return errors.Join(runErr, closeErr)
	}
	payload, err := execution.Finalize(signalCtx)
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return errors.New("adaptive benchmark report is empty")
	}
	if _, err := writeAdaptiveBenchmarkReport(
		request.OutputDir,
		payload,
	); err != nil {
		return err
	}
	line := append(append([]byte(nil), payload...), '\n')
	written, err := output.Write(line)
	if err != nil {
		return fmt.Errorf("write adaptive benchmark report: %w", err)
	}
	if written != len(line) {
		return fmt.Errorf(
			"write adaptive benchmark report: %w",
			io.ErrShortWrite,
		)
	}
	return nil
}

func parseAdaptiveBenchmarkRunArgs(
	args []string,
) (adaptiveBenchmarkRunRequest, error) {
	values := make(map[string]string, 3)
	confirmed := false
	for index := 0; index < len(args); index++ {
		token := args[index]
		if token == "--confirm-temporary-settings-override" {
			if confirmed {
				return adaptiveBenchmarkRunRequest{}, errors.New(
					"benchmark confirmation flag is duplicated",
				)
			}
			confirmed = true
			continue
		}
		name, value, consumed, err := parseAdaptiveBenchmarkValueFlag(
			args,
			index,
		)
		if err != nil {
			return adaptiveBenchmarkRunRequest{}, err
		}
		index += consumed
		if _, duplicate := values[name]; duplicate {
			return adaptiveBenchmarkRunRequest{}, fmt.Errorf(
				"benchmark flag --%s is duplicated",
				name,
			)
		}
		values[name] = value
	}
	if len(values) != 3 {
		return adaptiveBenchmarkRunRequest{}, errors.New(adaptiveBenchmarkRunUsage)
	}
	ownerID, err := parseAdaptiveBenchmarkUUID("owner-id", values["owner-id"])
	if err != nil {
		return adaptiveBenchmarkRunRequest{}, err
	}
	outputDir := values["output-dir"]
	if outputDir == "" ||
		!filepath.IsAbs(outputDir) ||
		filepath.Clean(outputDir) != outputDir {
		return adaptiveBenchmarkRunRequest{}, errors.New(
			"adaptive benchmark output-dir must be a canonical absolute path",
		)
	}
	mode := auraeval.AdaptiveBenchmarkMode(values["mode"])
	switch mode {
	case auraeval.AdaptiveBenchmarkModeQwenPortability:
		if !confirmed {
			return adaptiveBenchmarkRunRequest{}, errors.New(
				"qwen-portability requires --confirm-temporary-settings-override",
			)
		}
	case auraeval.AdaptiveBenchmarkModeProductionShadow:
		if confirmed {
			return adaptiveBenchmarkRunRequest{}, errors.New(
				"production-shadow rejects --confirm-temporary-settings-override",
			)
		}
	default:
		return adaptiveBenchmarkRunRequest{}, errors.New(
			"adaptive benchmark mode is invalid",
		)
	}
	return adaptiveBenchmarkRunRequest{
		OwnerID: ownerID, OutputDir: outputDir, Mode: mode,
		ConfirmedSettingsOverride: confirmed,
	}, nil
}

func parseAdaptiveBenchmarkValueFlag(
	args []string,
	index int,
) (name string, value string, consumed int, err error) {
	token := args[index]
	if !strings.HasPrefix(token, "--") || token == "--" {
		return "", "", 0, errors.New(
			"eval adaptive benchmark accepts exact flags only",
		)
	}
	flagText := strings.TrimPrefix(token, "--")
	if strings.Contains(flagText, "=") {
		name, value, _ = strings.Cut(flagText, "=")
	} else {
		name = flagText
		if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
			return "", "", 0, fmt.Errorf(
				"benchmark flag --%s requires a value",
				name,
			)
		}
		value = args[index+1]
		consumed = 1
	}
	switch name {
	case "mode", "owner-id", "output-dir":
	default:
		return "", "", 0, fmt.Errorf(
			"benchmark flag --%s is unknown",
			name,
		)
	}
	if value == "" {
		return "", "", 0, fmt.Errorf(
			"benchmark flag --%s requires a value",
			name,
		)
	}
	return name, value, consumed, nil
}
