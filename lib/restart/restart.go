package restart

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const HelperCommand = "__nps_restart_helper"

var (
	ErrNotConfigured  = errors.New("process restart is not configured")
	ErrAlreadyPending = errors.New("process restart is already pending")
)

type helperPayload struct {
	ParentPID   int      `json:"parent_pid"`
	Executable  string   `json:"executable"`
	Arguments   []string `json:"arguments"`
	WorkingDir  string   `json:"working_dir"`
	ServiceMode bool     `json:"service_mode"`
}

var runtimeState = struct {
	sync.Mutex
	configured bool
	pending    bool
	payload    helperPayload
}{}

func Configure(serviceMode bool) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	runtimeState.Lock()
	runtimeState.configured = true
	runtimeState.pending = false
	runtimeState.payload = helperPayload{
		ParentPID:   os.Getpid(),
		Executable:  executable,
		Arguments:   append([]string(nil), os.Args[1:]...),
		WorkingDir:  workingDir,
		ServiceMode: serviceMode,
	}
	runtimeState.Unlock()
	return nil
}

func Request() error {
	runtimeState.Lock()
	if !runtimeState.configured {
		runtimeState.Unlock()
		return ErrNotConfigured
	}
	if runtimeState.pending {
		runtimeState.Unlock()
		return ErrAlreadyPending
	}
	payload := runtimeState.payload
	runtimeState.pending = true
	runtimeState.Unlock()

	if !payload.ServiceMode && scheduleDirectExec(payload) {
		return nil
	}

	encoded, err := encodePayload(payload)
	if err != nil {
		clearPending()
		return err
	}
	command := exec.Command(payload.Executable, HelperCommand, encoded)
	command.Dir = payload.WorkingDir
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	prepareDetachedCommand(command)
	if err := command.Start(); err != nil {
		clearPending()
		return fmt.Errorf("start restart helper: %w", err)
	}

	if payload.ServiceMode {
		go func() {
			time.Sleep(45 * time.Second)
			clearPending()
		}()
	} else {
		go func() {
			time.Sleep(time.Second)
			os.Exit(0)
		}()
	}
	return nil
}

func RunHelper(encoded string) error {
	payload, err := decodePayload(encoded)
	if err != nil {
		return err
	}
	if payload.ParentPID <= 0 || payload.Executable == "" || payload.WorkingDir == "" {
		return errors.New("invalid restart helper payload")
	}

	time.Sleep(500 * time.Millisecond)
	if payload.ServiceMode {
		command := exec.Command(payload.Executable, serviceRestartArguments(payload.Arguments)...)
		command.Dir = payload.WorkingDir
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		prepareDetachedCommand(command)
		if err := command.Run(); err != nil {
			return fmt.Errorf("restart managed service: %w", err)
		}
		return nil
	}

	if err := waitForProcessExit(payload.ParentPID, 30*time.Second); err != nil {
		return err
	}
	command := exec.Command(payload.Executable, payload.Arguments...)
	command.Dir = payload.WorkingDir
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	prepareDetachedCommand(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("start replacement process: %w", err)
	}
	return nil
}

func encodePayload(payload helperPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode restart payload: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodePayload(encoded string) (helperPayload, error) {
	var payload helperPayload
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return payload, fmt.Errorf("decode restart payload: %w", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return payload, fmt.Errorf("parse restart payload: %w", err)
	}
	return payload, nil
}

func serviceRestartArguments(arguments []string) []string {
	result := []string{"restart"}
	for _, argument := range arguments {
		if strings.HasPrefix(argument, "-conf_path=") {
			result = append(result, argument)
		}
	}
	return result
}

func clearPending() {
	runtimeState.Lock()
	runtimeState.pending = false
	runtimeState.Unlock()
}
