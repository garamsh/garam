package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/lib/errorx"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/keyfile"
)

// The files a standalone host keeps (ADR-0082). The supervisor's
// directory holds what only the supervisor may read; the agent's
// credential directory holds what the agent's own adapter presents.
// Neither is the agent's memory, outbox or workspace.
const (
	// preparedFile holds the bootstrap secret and the request
	// identifier, written once before the approval is asked for.
	preparedFile = "prepared.json"
	// enrollmentFile records what the enrollment answered, last, and
	// never the credential.
	enrollmentFile = "enrollment.json"
	// credentialFile holds the supervisor's credential the enrollment
	// answered, which only the supervisor reads and only to exchange
	// (ADR-0100).
	credentialFile = "credential"
	serverRootFile = "server-root.pem"
)

// standaloneCallTimeout bounds one call to garam from a host command.
const standaloneCallTimeout = 30 * time.Second

// newStandaloneCommand constructs `garam standalone`: what a standalone
// host runs to stand its agent up without an operator. It prepares the
// bootstrap secret an administrator approves, spends the approval for
// the supervisor's credential, and runs the supervisor over the agent's
// workload (ADR-0085, ADR-0100).
func newStandaloneCommand() *cobra.Command {
	var dirs hostDirs
	cmd := &cobra.Command{
		Use:   "standalone",
		Short: "Enroll and run a standalone host's supervisor",
	}
	cmd.PersistentFlags().StringVar(&dirs.supervisor, "dir", "",
		"the supervisor's directory, readable by the supervisor alone (required)")
	_ = cmd.MarkPersistentFlagRequired("dir")

	prepare := &cobra.Command{
		Use:   "prepare",
		Short: "Generate the bootstrap secret an administrator approves",
		RunE: func(cmd *cobra.Command, _ []string) error {
			request, err := prepareHost(dirs)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(request)
		},
	}
	prepare.Flags().StringVar(&dirs.agent, "agent-dir", "", "the agent's credential directory (required)")
	_ = prepare.MarkFlagRequired("agent-dir")

	var machineURL, rootFile string
	enroll := &cobra.Command{
		Use:   "enroll",
		Short: "Spend the approval and install the supervisor's credential",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := enrollHost(cmd.Context(), dirs, machineURL, rootFile)
			if err != nil {
				return err
			}
			cmd.Printf("enrolled %s under supervisor %s at epoch %s\n", result.Grn, result.SupervisorGrn, result.Epoch)
			return nil
		},
	}
	enroll.Flags().StringVar(&dirs.agent, "agent-dir", "", "the agent's credential directory (required)")
	enroll.Flags().StringVar(&machineURL, "machine-url", "", "garam's machine listener (required)")
	enroll.Flags().StringVar(&rootFile, "server-root-file", "", "the garam server root to verify the listener by (required)")
	for _, name := range []string{"agent-dir", "machine-url", "server-root-file"} {
		_ = enroll.MarkFlagRequired(name)
	}

	cmd.AddCommand(prepare, enroll, newStandaloneRunCommand(&dirs))
	return cmd
}

// hostDirs is where a standalone host keeps the supervisor's files and
// the agent's credentials.
type hostDirs struct {
	supervisor string
	agent      string
}

// hostLockFile is what one standalone command holds in each directory
// it writes, so two never write one directory at once.
const hostLockFile = ".standalone.lock"

// lockDirs takes the lock of each directory, in the order given,
// without waiting, and answers what releases them. A directory another
// command holds refuses this one.
func lockDirs(dirs ...string) (func(), error) {
	var held []*os.File
	release := func() {
		for _, f := range held {
			_ = f.Close()
		}
	}
	for _, dir := range dirs {
		f, err := os.OpenFile(filepath.Join(dir, hostLockFile), os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			release()
			return nil, errorx.Wrap(err, "lock "+dir, errorx.KindInvalidArgument)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			_ = f.Close()
			release()
			return nil, errorx.Wrap(err, "another standalone command holds "+dir, errorx.KindFailedPrecondition)
		}
		held = append(held, f)
	}
	return release, nil
}

// resolvedDirs answers both directories' resolved paths, refusing one
// inside the other before creating either: the supervisor's files and
// the agent's credentials are never reachable through one directory.
func resolvedDirs(dirs hostDirs) (hostDirs, error) {
	if dirs.agent == "" {
		return hostDirs{}, errorx.New(errorx.KindInvalidArgument, "--agent-dir is required")
	}
	var out [2]string
	for i, dir := range []string{dirs.supervisor, dirs.agent} {
		resolved, err := resolvedPath(dir)
		if err != nil {
			return hostDirs{}, errorx.Wrap(err, "standalone directory", errorx.KindInvalidArgument)
		}
		out[i] = resolved
	}
	if within(out[0], out[1]) || within(out[1], out[0]) {
		return hostDirs{}, errorx.New(errorx.KindInvalidArgument, "--dir and --agent-dir must not overlap")
	}
	for _, dir := range out {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return hostDirs{}, errorx.Wrap(err, "standalone directory", errorx.KindInvalidArgument)
		}
	}
	return hostDirs{supervisor: out[0], agent: out[1]}, nil
}

// resolvedPath answers dir with every symbolic link in its existing
// part resolved, whether or not dir itself exists yet.
func resolvedPath(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	missing := ""
	for {
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			return filepath.Join(resolved, missing), nil
		}
		if !errors.Is(err, fs.ErrNotExist) || filepath.Dir(abs) == abs {
			return "", err
		}
		missing = filepath.Join(filepath.Base(abs), missing)
		abs = filepath.Dir(abs)
	}
}

// within reports whether path is dir or below it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// prepared is what a host generated before asking for its approval.
// The secret never leaves the host. AgentDir is the one agent directory
// this preparation owns.
type prepared struct {
	RequestID string `json:"requestId"`
	Secret    string `json:"secret"`
	AgentDir  string `json:"agentDir"`
}

// approvalRequest is what an administrator approves: the secret's hash
// and the request identifier, and no key (ADR-0100, ADR-0118).
type approvalRequest struct {
	RequestID    string `json:"requestId"`
	SecretSHA256 string `json:"secretSha256"`
}

// prepareHost generates the bootstrap secret and request identifier
// of an approval, durably, and answers what to approve. Run again it
// answers the same request from what it wrote, so an approval is never
// asked for twice.
func prepareHost(dirs hostDirs) (*approvalRequest, error) {
	dirs, err := resolvedDirs(dirs)
	if err != nil {
		return nil, err
	}
	release, err := lockDirs(dirs.supervisor, dirs.agent)
	if err != nil {
		return nil, err
	}
	defer release()
	p, err := readPrepared(dirs.supervisor)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if p, err = newPrepared(dirs); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case p.AgentDir != dirs.agent:
		return nil, errorx.New(errorx.KindFailedPrecondition, "this preparation owns the agent directory "+p.AgentDir)
	}
	return p.request(), nil
}

// newPrepared generates the secret and request identifier and writes
// them in the one record that commits the preparation.
func newPrepared(dirs hostDirs) (*prepared, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, errorx.Wrap(err, "bootstrap secret", errorx.KindInternal)
	}
	p := &prepared{
		RequestID: uuid.NewString(),
		Secret:    base64.RawURLEncoding.EncodeToString(secret),
		AgentDir:  dirs.agent,
	}
	return p, p.write(dirs.supervisor)
}

func (p *prepared) write(dir string) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return errorx.Wrap(err, "prepared", errorx.KindInternal)
	}
	return writeDurably(filepath.Join(dir, preparedFile), raw, 0o600)
}

// readPrepared answers what prepareHost wrote, or an fs.ErrNotExist
// error where it wrote nothing yet.
func readPrepared(dir string) (*prepared, error) {
	raw, err := keyfile.Read(filepath.Join(dir, preparedFile))
	if err != nil {
		return nil, err
	}
	var p prepared
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errorx.Wrap(err, "prepared", errorx.KindInvalidArgument)
	}
	return &p, nil
}

func (p *prepared) request() *approvalRequest {
	secret := sha256.Sum256([]byte(p.Secret))
	return &approvalRequest{RequestID: p.RequestID, SecretSHA256: hex.EncodeToString(secret[:])}
}

// enrollHost spends the approval of what prepareHost wrote and installs
// the supervisor's credential. Run again after an interruption it sends
// the same bytes, and installs the credential garam re-issues, which
// voids the one an interrupted run may have written. Once the
// enrollment record exists it installs nothing, so no replay ever
// replaces a credential re-issued since.
func enrollHost(ctx context.Context, dirs hostDirs, machineURL, rootFile string) (*machinegen.StandaloneEnrollment, error) {
	base, err := machineEndpoint(machineURL)
	if err != nil {
		return nil, err
	}
	dirs, err = resolvedDirs(dirs)
	if err != nil {
		return nil, err
	}
	release, err := lockDirs(dirs.supervisor, dirs.agent)
	if err != nil {
		return nil, err
	}
	defer release()
	p, err := readPrepared(dirs.supervisor)
	if err != nil {
		return nil, errorx.Wrap(err, "run `garam standalone prepare` first", errorx.KindFailedPrecondition)
	}
	if p.AgentDir != dirs.agent {
		return nil, errorx.New(errorx.KindFailedPrecondition, "the preparation is for another agent directory")
	}
	if done, err := readEnrollment(dirs.supervisor); err == nil {
		return done, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	rootPEM, err := os.ReadFile(rootFile)
	if err != nil {
		return nil, errorx.Wrap(err, "server root", errorx.KindInvalidArgument)
	}
	client, err := standaloneClient(rootPEM)
	if err != nil {
		return nil, err
	}
	return enrollCredential(ctx, dirs, p, client, base)
}

// enrollCredential spends the approval: the answer is the supervisor's
// credential and the garam server root, and nothing is signed. The
// credential is written to its own owner-only file before the record,
// and never into the record (ADR-0100).
func enrollCredential(ctx context.Context, dirs hostDirs, p *prepared, client *http.Client, base string) (*machinegen.StandaloneEnrollment, error) {
	body, err := json.Marshal(machinegen.StandaloneEnrollmentRequest{RequestId: p.RequestID})
	if err != nil {
		return nil, errorx.Wrap(err, "enrollment", errorx.KindInternal)
	}
	var result machinegen.StandaloneEnrollment
	if _, err := callStandalone(ctx, client, base+"/agent-enrollments", agent.CredentialStandaloneContractVersion,
		"Garam-Bootstrap "+p.Secret, body, &result); err != nil {
		return nil, err
	}
	if !supervisorCredential.MatchString(result.Credential) {
		return nil, errorx.New(errorx.KindInternal,
			"garam answered a "+agent.CredentialStandaloneContractVersion+" enrollment without a supervisor credential")
	}
	writes := []struct {
		path string
		data []byte
		perm fs.FileMode
	}{
		{filepath.Join(dirs.supervisor, credentialFile), []byte(result.Credential), 0o600},
		{filepath.Join(dirs.agent, serverRootFile), []byte(result.ServerRootPem), 0o644},
		{filepath.Join(dirs.supervisor, serverRootFile), []byte(result.ServerRootPem), 0o644},
	}
	for _, w := range writes {
		if err := writeDurably(w.path, w.data, w.perm); err != nil {
			return nil, err
		}
	}
	result.Credential = ""
	record, err := json.Marshal(result)
	if err != nil {
		return nil, errorx.Wrap(err, "enrollment record", errorx.KindInternal)
	}
	return &result, writeDurably(filepath.Join(dirs.supervisor, enrollmentFile), record, 0o600)
}

// supervisorCredential is a supervisor credential as api/machine.yaml
// spells one.
var supervisorCredential = regexp.MustCompile(`^garam_sc_[0-9a-f]{64}$`)

// readEnrollment answers what a completed enrollment recorded, or an
// fs.ErrNotExist error before one completed.
func readEnrollment(dir string) (*machinegen.StandaloneEnrollment, error) {
	raw, err := keyfile.Read(filepath.Join(dir, enrollmentFile))
	if err != nil {
		return nil, err
	}
	var e machinegen.StandaloneEnrollment
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, errorx.Wrap(err, "enrollment record", errorx.KindInvalidArgument)
	}
	return &e, nil
}

// machineEndpoint answers garam's machine listener as a base URL, and
// refuses anything but an https:// origin before a credential is read:
// every request carries one, and none crosses plaintext or reaches
// another host.
func machineEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		strings.Trim(u.Path, "/") != "" {
		return "", errorx.New(errorx.KindInvalidArgument, "--machine-url must be an https:// origin")
	}
	return "https://" + u.Host, nil
}

// standaloneClient reaches garam's machine listener, verifying it by
// the garam server root (ADR-0040).
func standaloneClient(rootPEM []byte) (*http.Client, error) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		return nil, errorx.New(errorx.KindInvalidArgument, "server root: no certificate")
	}
	config := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: config},
		Timeout:   standaloneCallTimeout,
		// A redirect is answered as the refusal it is, so a credential
		// is never sent to a location the endpoint names.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

// callStandalone posts body under contract and decodes a 200 or 201
// answer into out, answering which of the two it was. Any other answer
// is an error carrying the kind garam named, and never the
// authorization it was sent with.
func callStandalone(ctx context.Context, client *http.Client, url, contract, authorization string, body []byte, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, errorx.Wrap(err, "request", errorx.KindInvalidArgument)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Garam-Contract-Version", contract)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, errorx.Wrap(err, "garam", errorx.KindUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, errorx.Wrap(err, "garam answer", errorx.KindUnavailable)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var refusal machinegen.Error
		_ = json.Unmarshal(raw, &refusal)
		return resp.StatusCode, errorx.New(kindOfStatus(resp.StatusCode),
			"garam answered "+resp.Status+": "+strings.TrimSpace(refusal.Message))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return resp.StatusCode, errorx.Wrap(err, "garam answer", errorx.KindInternal)
	}
	return resp.StatusCode, nil
}

// kindOfStatus names the kind a refusal's status carries back.
func kindOfStatus(status int) errorx.Kind {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return errorx.KindInvalidArgument
	case http.StatusUnauthorized:
		return errorx.KindUnauthenticated
	case http.StatusForbidden:
		return errorx.KindPermissionDenied
	case http.StatusNotFound:
		return errorx.KindNotFound
	case http.StatusConflict:
		return errorx.KindFailedPrecondition
	}
	return errorx.KindUnavailable
}

// writeDurably replaces path with data in one rename, after the data
// and then the directory entry reach the disk, so a crash leaves either
// the old file or the new one and never a part of either.
func writeDurably(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return errorx.Wrap(err, "write "+path, errorx.KindInvalidArgument)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return errorx.Wrap(err, "write "+path, errorx.KindInvalidArgument)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return errorx.Wrap(err, "write "+path, errorx.KindInvalidArgument)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errorx.Wrap(err, "write "+path, errorx.KindInvalidArgument)
	}
	if err := tmp.Close(); err != nil {
		return errorx.Wrap(err, "write "+path, errorx.KindInvalidArgument)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errorx.Wrap(err, "write "+path, errorx.KindInvalidArgument)
	}
	return syncDir(dir)
}

// removeDurably removes path and makes the removal reach the disk.
func removeDurably(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errorx.Wrap(err, "remove "+path, errorx.KindInvalidArgument)
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return errorx.Wrap(err, "sync "+dir, errorx.KindInvalidArgument)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return errorx.Wrap(err, "sync "+dir, errorx.KindInvalidArgument)
	}
	return nil
}
