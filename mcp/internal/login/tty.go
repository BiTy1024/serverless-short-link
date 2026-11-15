package login

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

// TTY prompts on the controlling terminal (/dev/tty), so stdin and stdout
// stay free for piping. Echo is switched off for secrets with `stty -echo`,
// which keeps the binary free of terminal libraries.
type TTY struct {
	f  *os.File
	rd *bufio.Reader
}

// OpenTTY opens the controlling terminal.
func OpenTTY() (*TTY, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("login needs an interactive terminal: %w", err)
	}
	return &TTY{f: f, rd: bufio.NewReader(f)}, nil
}

// Close releases the terminal.
func (t *TTY) Close() error { return t.f.Close() }

// ReadLine implements Prompter.
func (t *TTY) ReadLine(prompt string) (string, error) {
	if _, err := t.f.WriteString(prompt); err != nil {
		return "", err
	}
	return t.readLine()
}

// ReadSecret implements Prompter. Echo is restored on return and on SIGINT
// or SIGTERM while waiting for input.
func (t *TTY) ReadSecret(prompt string) (string, error) {
	if _, err := t.f.WriteString(prompt); err != nil {
		return "", err
	}
	if err := t.stty("-echo"); err != nil {
		return "", fmt.Errorf("cannot switch off terminal echo: %w", err)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-sigs:
			_ = t.stty("echo")
			_, _ = t.f.WriteString("\n")
			os.Exit(130)
		case <-done:
		}
	}()
	defer func() {
		close(done)
		signal.Stop(sigs)
		_ = t.stty("echo")
		_, _ = t.f.WriteString("\n")
	}()

	return t.readLine()
}

func (t *TTY) readLine() (string, error) {
	line, err := t.rd.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (t *TTY) stty(arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = t.f
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
