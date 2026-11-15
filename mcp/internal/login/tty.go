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
	"sync"
	"syscall"
)

// TTY prompts on the controlling terminal (/dev/tty), so stdin and stdout
// stay free for piping. Echo is switched off for secrets with `stty`, which
// keeps the binary free of terminal libraries.
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

// ReadSecret implements Prompter. The terminal state is saved with
// `stty -g` before echo is switched off, and exactly that state is restored
// on every return and on SIGINT, SIGTERM, SIGHUP or SIGQUIT.
func (t *TTY) ReadSecret(prompt string) (string, error) {
	saved, err := t.sttyOutput("-g")
	if err != nil {
		return "", fmt.Errorf("cannot read terminal state: %w", err)
	}
	var once sync.Once
	restore := func() {
		once.Do(func() {
			_ = t.stty(saved)
			_, _ = t.f.WriteString("\n")
		})
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-sigs:
			restore()
			os.Exit(128 + int(sig.(syscall.Signal)))
		case <-done:
		}
	}()
	defer func() {
		close(done)
		signal.Stop(sigs)
		restore()
	}()

	if _, err := t.f.WriteString(prompt); err != nil {
		return "", err
	}
	if err := t.stty("-echo"); err != nil {
		return "", fmt.Errorf("cannot switch off terminal echo: %w", err)
	}
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

func (t *TTY) sttyOutput(arg string) (string, error) {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = t.f
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
