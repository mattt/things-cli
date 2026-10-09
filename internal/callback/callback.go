// Package callback provides private, correlated, one-shot URL acknowledgments.
package callback

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mattt/things-cli/internal/domain"
)

const Scheme = "things-cli-callback"

var hexID = regexp.MustCompile(`^[a-f0-9]{32}$`)

var itemID = regexp.MustCompile(`^[A-Za-z0-9-]{1,128}$`)

type Result struct {
	Outcome string
	IDs     []string
}

type Listener struct {
	URLs     map[string]string
	listener net.Listener
	path     string
	results  chan Result
	mu       sync.Mutex
	active   bool
	used     bool
}

func socketPath(id string) (string, error) {
	if !hexID.MatchString(id) {
		return "", domain.E("invalid_callback", "Invalid callback operation ID.", 2)
	}
	return filepath.Join("/tmp", fmt.Sprintf("things-cli-%d", os.Getuid()), id+".sock"), nil
}

func Start(id string) (*Listener, error) {
	path, err := socketPath(id)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, domain.E("callback_directory", "Callback IPC requires a private directory.", 4)
	}

	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	nonce := hex.EncodeToString(secret[:])

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		ln.Close()
		os.Remove(path)
		return nil, err
	}

	l := &Listener{
		URLs:     map[string]string{},
		listener: ln,
		path:     path,
		results:  make(chan Result, 1),
	}
	for _, outcome := range []string{"success", "error", "cancel"} {
		q := url.Values{"operation": {id}, "nonce": {nonce}}
		l.URLs["x-"+outcome] = Scheme + ":///" + outcome + "?" + q.Encode()
	}

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}

			func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(2 * time.Second))
				b, err := io.ReadAll(io.LimitReader(c, 16385))
				if err != nil || len(b) > 16384 {
					return
				}

				r, n, op, err := parse(string(b))
				if err != nil || op != id || subtle.ConstantTimeCompare([]byte(n), []byte(nonce)) != 1 {
					return
				}

				l.mu.Lock()
				defer l.mu.Unlock()
				if !l.active || l.used {
					return
				}
				l.used = true
				l.results <- r
			}()
		}
	}()

	return l, nil
}

func (l *Listener) Activate() {
	l.mu.Lock()
	l.active = true
	l.mu.Unlock()
}

func (l *Listener) Take() *Result {
	if l == nil {
		return nil
	}
	select {
	case r := <-l.results:
		return &r
	default:
		return nil
	}
}

func (l *Listener) Close() {
	if l == nil {
		return
	}

	l.mu.Lock()
	l.active = false
	l.mu.Unlock()

	l.listener.Close()
	os.Remove(l.path)
}

func parse(value string) (Result, string, string, error) {
	if len(value) > 16384 {
		return Result{}, "", "", domain.E("invalid_callback", "The callback is too large.", 2)
	}

	u, err := url.Parse(value)
	if err != nil || u.Scheme != Scheme || u.Host != "" || u.Fragment != "" {
		return Result{}, "", "", domain.E("invalid_callback", "Invalid callback URL.", 2)
	}
	outcome := strings.TrimPrefix(u.Path, "/")
	if outcome != "success" && outcome != "error" && outcome != "cancel" {
		return Result{}, "", "", domain.E("invalid_callback", "Invalid callback outcome.", 2)
	}

	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Result{}, "", "", domain.E("invalid_callback", "Invalid callback query.", 2)
	}
	for _, k := range []string{"operation", "nonce", "x-things-id", "x-things-ids"} {
		if len(q[k]) > 1 {
			return Result{}, "", "", domain.E("invalid_callback", "Duplicate callback parameter.", 2)
		}
	}

	id, nonce := q.Get("operation"), q.Get("nonce")
	if !hexID.MatchString(id) || len(nonce) != 64 {
		return Result{}, "", "", domain.E("invalid_callback", "Invalid callback correlation.", 2)
	}
	if _, err := hex.DecodeString(nonce); err != nil {
		return Result{}, "", "", domain.E("invalid_callback", "Invalid callback correlation.", 2)
	}

	ids := []string{}
	if raw := q.Get("x-things-id"); raw != "" {
		ids = strings.Split(raw, ",")
	}
	if raw := q.Get("x-things-ids"); raw != "" {
		if len(ids) > 0 || json.Unmarshal([]byte(raw), &ids) != nil {
			return Result{}, "", "", domain.E("invalid_callback", "Invalid callback IDs.", 2)
		}
	}
	if len(ids) > 250 {
		return Result{}, "", "", domain.E("invalid_callback", "Too many callback IDs.", 2)
	}

	seen := map[string]bool{}
	for _, id := range ids {
		if !itemID.MatchString(id) || seen[id] {
			return Result{}, "", "", domain.E("invalid_callback", "Invalid or duplicate returned ID.", 2)
		}
		seen[id] = true
	}

	return Result{Outcome: outcome, IDs: ids}, nonce, id, nil
}

func Forward(value string) error {
	_, _, id, err := parse(value)
	if err != nil {
		return err
	}
	path, err := socketPath(id)
	if err != nil {
		return err
	}

	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return domain.E("callback_expired", "No command is waiting for this callback.", 3)
	}
	defer c.Close()
	c.SetWriteDeadline(time.Now().Add(time.Second))
	_, err = io.WriteString(c, value)
	return err
}
