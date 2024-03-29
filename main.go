package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

func main() {
	// Initialize logger with default level as Info
	logger, err := initLogger(zap.InfoLevel)
	if err != nil {
		panic(err)
	}
	logger.Info("Logger initialized")

	// Parse the command-line flag for the user command
	var userCmd string
	flag.StringVar(&userCmd, "cmd", "echo Please provide a command to run!", "Command to run")
	flag.Parse()

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up a signal channel to listen for termination signals
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// Run a goroutine that listens for signals and cancels the context
	go func() {
		sig := <-signalChan
		logger.Info("Received signal, canceling context", zap.String("signal", sig.String()))
		cancel()
	}()

	// Call your function with the cancellable context
	if err := run(ctx, logger, userCmd); err != nil {
		logger.Error("Failed to run the command", zap.Error(err))
	}
}

func run(ctx context.Context, logger *zap.Logger, userCmd string) error {
	// Run the app as the user who invoked sudo
	username := os.Getenv("SUDO_USER")

	cmd := exec.CommandContext(ctx, "sh", "-c", userCmd)
	if username != "" {
		// print all environment variables
		logger.Debug("env inherited from the cmd", zap.Any("env", os.Environ()))
		// Run the command as the user who invoked sudo to preserve the user environment variables and PATH
		cmd = exec.CommandContext(ctx, "sudo", "-E", "-u", os.Getenv("SUDO_USER"), "env", "PATH="+os.Getenv("PATH"), "sh", "-c", userCmd)
	}

	// Set the cancel function for the command
	cmd.Cancel = func() error {

		return interruptProcessTree(logger, cmd.Process.Pid, syscall.SIGINT)
	}
	// wait after sending the interrupt signal, before sending the kill signal
	cmd.WaitDelay = 10 * time.Second

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Set the output of the command
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logger.Debug("", zap.Any("executing cli", cmd.String()))

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start the app: %w", err)
	}

	err = cmd.Wait()
	select {
	case <-ctx.Done():
		logger.Debug("context cancelled, error while waiting for the app to exit", zap.Error(ctx.Err()))
		return ctx.Err()
	default:
		if err != nil {
			return fmt.Errorf("unexpected error while waiting for the app to exit: %w", err)
		}
		logger.Debug("app exited successfully")
		return nil
	}
}
