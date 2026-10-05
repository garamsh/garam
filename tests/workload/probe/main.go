// Command probe is what the real-engine suite runs inside a container
// of a generation, as a process of that container: what it can see and
// reach, a daemonized child that ignores SIGTERM, and a request to the
// runtime's loopback gateway. It is built static, so it runs in each
// of the three images, a distroless and a scratch one among them.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fail("usage: probe report|daemon|post|get|size ...")
	}
	var err error
	switch os.Args[1] {
	case "report":
		err = report(os.Args[2:])
	case "daemon":
		err = daemon(os.Args[2:])
	case "post":
		err = post(os.Args[2], os.Args[3])
	case "get":
		err = get(os.Args[2])
	case "size":
		err = size(os.Args[2])
	default:
		err = fmt.Errorf("unknown mode %q", os.Args[1])
	}
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

// Report is what one process of a container can see and reach.
type Report struct {
	Status map[string]string `json:"status"`
	Env    []string          `json:"env"`
	Mounts []string          `json:"mounts"`
	Open   map[string]string `json:"open"`
	Write  map[string]string `json:"write"`
}

// report reads its own credentials from /proc/self/status, the names
// of its environment, its mount points, and whether each --open path
// opens (a socket is dialled) and each --write directory takes a file
// it then reads back.
func report(args []string) error {
	r := Report{Status: map[string]string{}, Open: map[string]string{}, Write: map[string]string{}}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(status), "\n") {
		key, value, ok := strings.Cut(line, ":")
		switch key {
		case "Uid", "Gid", "Groups", "CapEff", "CapPrm", "CapBnd", "NoNewPrivs":
			if ok {
				r.Status[key] = strings.Join(strings.Fields(value), " ")
			}
		}
	}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		r.Env = append(r.Env, key)
	}
	mounts, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	lines := bufio.NewScanner(mounts)
	for lines.Scan() {
		if fields := strings.Fields(lines.Text()); len(fields) > 4 {
			r.Mounts = append(r.Mounts, fields[4])
		}
	}
	_ = mounts.Close()
	for i := 0; i+1 < len(args); i += 2 {
		switch args[i] {
		case "--open":
			r.Open[args[i+1]] = outcome(reach(args[i+1]))
		case "--write":
			r.Write[args[i+1]] = outcome(roundTrip(args[i+1]))
		}
	}
	return json.NewEncoder(os.Stdout).Encode(r)
}

func outcome(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

func reach(path string) error {
	if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
		conn, err := net.DialTimeout("unix", path, time.Second)
		if err == nil {
			_ = conn.Close()
		}
		return err
	}
	f, err := os.Open(path)
	if err == nil {
		_ = f.Close()
	}
	return err
}

// roundTrip makes dir where it is missing, as the runtime's message
// tool makes the outbox, then writes a file there and reads it back.
func roundTrip(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("probe-%d", time.Now().UnixNano()))
	want := []byte("written by the probe")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		return err
	}
	defer func() { _ = os.Remove(path) }()
	got, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("read back other bytes")
	}
	return nil
}

// daemon detaches twice, each time in a new session, so the process
// left running is no child of anything the caller started; that
// process ignores SIGTERM and appends a line to its file every tenth
// of a second for as long as it lives.
func daemon(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: probe daemon <file>")
	}
	switch os.Getenv("PROBE_STAGE") {
	case "":
		return detach("1")
	case "1":
		return detach("2")
	}
	signal.Ignore(syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	f, err := os.OpenFile(args[0], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	for {
		if _, err := fmt.Fprintln(f, time.Now().UnixNano()); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func detach(stage string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "PROBE_STAGE="+stage)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func post(url, body string) error {
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	fmt.Println(resp.StatusCode)
	return nil
}

func get(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, err = io.Copy(os.Stdout, resp.Body)
	return err
}

func size(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	fmt.Println(info.Size())
	return nil
}
