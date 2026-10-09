package callback

import (
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestCorrelatedOneShotCallbacks(t *testing.T) {
	l, err := Start("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Activate()

	raw := l.URLs["x-success"] + "&x-things-id=created"
	bad, _ := url.Parse(raw)
	q := bad.Query()
	q.Set("nonce", "0000000000000000000000000000000000000000000000000000000000000000")
	bad.RawQuery = q.Encode()
	if err := Forward(bad.String()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if l.Take() != nil {
		t.Fatal("uncorrelated callback accepted")
	}

	if err := Forward(raw); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	var result *Result
	for result == nil && time.Now().Before(deadline) {
		result = l.Take()
		time.Sleep(time.Millisecond)
	}
	if result == nil || result.Outcome != "success" || len(result.IDs) != 1 || result.IDs[0] != "created" {
		t.Fatal(result)
	}

	_ = Forward(raw)
	time.Sleep(10 * time.Millisecond)
	if l.Take() != nil {
		t.Fatal("callback replay accepted")
	}
}

func TestUnsolicitedAndMalformedCallbacks(t *testing.T) {
	for _, v := range []string{
		"https://bad.example",
		"things-cli-callback:///success?operation=../../etc/passwd",
		"things-cli-callback:///success?operation=0123456789abcdef0123456789abcdef&nonce=bad",
	} {
		if Forward(v) == nil {
			t.Fatal("invalid callback accepted")
		}
	}
}

func TestNativeHelperDelivery(t *testing.T) {
	path := os.Getenv("THINGS_TEST_HELPER")
	if path == "" || runtime.GOOS != "darwin" {
		t.Skip("set THINGS_TEST_HELPER to run the native URL-event check")
	}

	l, err := Start("11111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Activate()

	raw := l.URLs["x-success"] + "&x-things-id=synthetic-created-id"
	if err := exec.Command("/usr/bin/open", "-a", path, raw).Run(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r := l.Take(); r != nil {
			if r.Outcome != "success" || len(r.IDs) != 1 || r.IDs[0] != "synthetic-created-id" {
				t.Fatal(r)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("AppKit did not forward the synthetic callback")
}
