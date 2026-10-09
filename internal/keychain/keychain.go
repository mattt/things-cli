// Package keychain keeps Things update tokens outside configuration and journals.
package keychain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/mattt/things-cli/internal/domain"
)

var ErrMissing = domain.E("token_missing", "No update token is stored. Run things auth set.", 4)

type Store interface {
	Token(context.Context) (string, error)
}

type Keychain struct{}

const service = "com.mattt.things-cli"

func Available() bool { return runtime.GOOS == "darwin" }

func validToken(token string) bool {
	if len(token) < 1 || len(token) > 4096 {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (Keychain) Token(ctx context.Context) (string, error) {
	if token := os.Getenv("THINGS_AUTH_TOKEN"); token != "" {
		if !validToken(token) {
			return "", domain.E("token_invalid", "THINGS_AUTH_TOKEN contains an invalid token.", 4)
		}
		return token, nil
	}

	if !Available() {
		return "", domain.E("keychain_unavailable", "macOS Keychain is unavailable on this platform.", 4)
	}
	return storedToken(ctx)
}

func storedToken(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-a", "things", "-s", service, "-w").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return "", ErrMissing
		}
		return "", domain.E("keychain_access", "Cannot access the update token. Unlock Keychain and allow access for this command.", 4)
	}

	token := strings.TrimSpace(string(out))
	if !validToken(token) {
		return "", domain.E("token_invalid", "Keychain contains an invalid Things token.", 4)
	}
	return token, nil
}

func Set(ctx context.Context, token string) error {
	if !Available() {
		return domain.E("keychain_unavailable", "macOS Keychain is unavailable on this platform.", 4)
	}
	if !validToken(token) {
		return domain.E("token_invalid", "The Things token must contain only letters, digits, hyphens, and underscores.", 2)
	}

	// security's interactive mode takes the secret over stdin, never in argv.
	c := exec.CommandContext(ctx, "/usr/bin/security", "-i")
	c.Stdin = strings.NewReader("add-generic-password -U -a things -s " + service + " -w " + token + "\n")
	if _, err := c.Output(); err != nil {
		return domain.E("keychain_access", "Cannot save the token. Unlock Keychain and allow access.", 4)
	}

	if saved, err := storedToken(ctx); err != nil || saved != token {
		return domain.E("keychain_access", "Keychain did not confirm that the token was saved.", 4)
	}
	return nil
}

func Delete(ctx context.Context) error {
	if !Available() {
		return domain.E("keychain_unavailable", "macOS Keychain is unavailable on this platform.", 4)
	}

	if err := exec.CommandContext(ctx, "/usr/bin/security", "delete-generic-password", "-a", "things", "-s", service).Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return nil
		}
		return domain.E("keychain_access", "Cannot delete the token. Unlock Keychain and allow access.", 4)
	}
	return nil
}
