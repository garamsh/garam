package cli

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
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

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/keyfile"
)

// The files a standalone host keeps (ADR-0082). The supervisor's
// directory holds what only the supervisor may read; the agent's
// credential directory holds what the agent's own adapter presents.
// Neither is the agent's memory, outbox or workspace.
const (
	// preparedFile holds the bootstrap secret, the request identifier
	// and the supervisor's first key and request, written once before
	// the approval is asked for.
	preparedFile = "prepared.json"
	// supervisorFile holds the supervisor's current certificate and
	// its key in one file, so a renewal replaces both in one rename.
	supervisorFile = "supervisor.pem"
	// renewalFile holds a renewal's key and request from before the
	// request is sent until its answer is installed.
	renewalFile = "renewal.json"
	// enrollmentFile records what the enrollment answered, last, and
	// never the credential: its supervisor certificate or its absence
	// is what says which contract the host enrolled under.
	enrollmentFile = "enrollment.json"
	// credentialFile holds the supervisor's credential a
	// standalone-enrollment.v2 enrollment answered, which only the
	// supervisor reads and only to exchange (ADR-0100).
	credentialFile = "credential"
	serverRootFile = "server-root.pem"
	agentKeyFile   = "agent.key"
	agentCSRFile   = "agent.csr"
	agentCertFile  = "agent.crt"
	issuerFile     = "issuer.pem"
)

// standaloneCallTimeout bounds one call to garam from a host command.
const standaloneCallTimeout = 30 * time.Second

// newStandaloneCommand constructs `garam standalone`: what a standalone
// host runs to stand its agent up without an operator. It prepares the
// bootstrap secret an administrator approves, spends the approval for
// the supervisor's credential, renews a v1 host's certificate, and runs
// the supervisor over the agent's workload (ADR-0085, ADR-0100).
func newStandaloneCommand() *cobra.Command {
	var dirs hostDirs
	cmd := &cobra.Command{
		Use:   "standalone",
		Short: "Enroll, renew and run a standalone host's supervisor",
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

	renew := &cobra.Command{
		Use:   "renew",
		Short: "Renew a supervisor certificate a v1 enrollment installed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			notAfter, err := renewSupervisor(cmd.Context(), dirs.supervisor, machineURL)
			if err != nil {
				return err
			}
			cmd.Printf("supervisor certificate valid until %s\n", notAfter.UTC().Format(time.RFC3339))
			return nil
		},
	}
	renew.Flags().StringVar(&machineURL, "machine-url", "", "garam's machine listener (required)")
	_ = renew.MarkFlagRequired("machine-url")

	cmd.AddCommand(prepare, enroll, renew, newStandaloneRunCommand(&dirs))
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
// The secret and the keys never leave the host. A preparation holding
// keys is one for standalone-enrollment.v1, which this command no
// longer writes but still finishes; one holding none is for v2, which
// binds no key (ADR-0100). AgentKeyPEM is held only until the agent's
// files are written beside AgentDir, the one directory this
// preparation owns them in.
type prepared struct {
	RequestID              string `json:"requestId"`
	Secret                 string `json:"secret"`
	SupervisorKeyPEM       string `json:"supervisorKeyPem,omitempty"`
	SupervisorRequestPEM   string `json:"supervisorCertificateRequestPem,omitempty"`
	AgentDir               string `json:"agentDir"`
	AgentKeyPEM            string `json:"agentKeyPem,omitempty"`
	AgentRequestPEM        string `json:"agentCertificateRequestPem,omitempty"`
	AgentKeyFingerprintHex string `json:"agentKeySha256,omitempty"`
}

// keyed reports a preparation for standalone-enrollment.v1.
func (p *prepared) keyed() bool { return p.SupervisorRequestPEM != "" }

// approvalRequest is what an administrator approves: hashes and
// fingerprints alone, and under v2 no fingerprint.
type approvalRequest struct {
	RequestID           string `json:"requestId"`
	SecretSHA256        string `json:"secretSha256"`
	SupervisorKeySHA256 string `json:"supervisorKeySha256,omitempty"`
	AgentKeySHA256      string `json:"agentKeySha256,omitempty"`
}

// prepareHost generates the bootstrap secret and request identifier
// of a standalone-enrollment.v2 approval, durably, and answers what to
// approve. Run again it answers the same request from what it wrote,
// so an approval is never asked for twice. A preparation an earlier
// release wrote with keys is answered and finished as it was: its
// approval binds those keys, so they are never regenerated, and a run
// interrupted before the agent's files were written finishes. It
// writes no agent credential it did not generate over one already
// there.
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
		for _, name := range []string{agentKeyFile, agentCSRFile, agentCertFile} {
			if _, err := os.Lstat(filepath.Join(dirs.agent, name)); !errors.Is(err, fs.ErrNotExist) {
				return nil, errorx.New(errorx.KindFailedPrecondition,
					"the agent directory already holds "+name+", which this preparation did not write")
			}
		}
		if p, err = newPrepared(dirs); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case p.AgentDir != dirs.agent:
		return nil, errorx.New(errorx.KindFailedPrecondition, "this preparation owns the agent directory "+p.AgentDir)
	}
	if err := installAgentFiles(dirs, p); err != nil {
		return nil, err
	}
	return p.request()
}

// newPrepared generates the secret and request identifier and writes
// them in the one record that commits the preparation. A v2 approval
// binds no key, so none is generated.
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

// installAgentFiles writes a v1 preparation's agent key and request
// where the record still holds the key, and then drops it from the
// record. Where the record no longer holds it, the files must be the
// prepared ones. A v2 preparation has none to write.
//
// It never replaces a file: one already there is accepted only when it
// holds exactly the record's bytes — this preparation's own interrupted
// write — and anything else refuses before either file is written, so
// resuming never takes over an agent directory another preparation
// filled meanwhile.
func installAgentFiles(dirs hostDirs, p *prepared) error {
	if !p.keyed() {
		return nil
	}
	if p.AgentKeyPEM != "" {
		files := []struct {
			path string
			data []byte
			perm fs.FileMode
		}{
			{filepath.Join(dirs.agent, agentKeyFile), []byte(p.AgentKeyPEM), 0o600},
			{filepath.Join(dirs.agent, agentCSRFile), []byte(p.AgentRequestPEM), 0o644},
		}
		for _, f := range files {
			if err := ownedOrAbsent(f.path, f.data); err != nil {
				return err
			}
		}
		for _, f := range files {
			if err := createDurably(f.path, f.data, f.perm); err != nil {
				return err
			}
		}
		p.AgentKeyPEM = ""
		if err := p.write(dirs.supervisor); err != nil {
			return err
		}
	}
	requestPEM, err := os.ReadFile(filepath.Join(dirs.agent, agentCSRFile))
	if err != nil {
		return errorx.Wrap(err, "prepared agent request", errorx.KindFailedPrecondition)
	}
	agentKey, err := agent.RequestedKeyFingerprint(requestPEM)
	if err != nil || agentKey.String() != p.AgentKeyFingerprintHex {
		return errorx.New(errorx.KindFailedPrecondition, "the agent directory does not hold the prepared agent key")
	}
	return nil
}

// ownedOrAbsent refuses a file at path holding anything but data.
func ownedOrAbsent(path string, data []byte) error {
	existing, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return errorx.Wrap(err, "read "+path, errorx.KindInvalidArgument)
	case !bytes.Equal(existing, data):
		return errorx.New(errorx.KindFailedPrecondition, path+" holds a credential this preparation did not write")
	}
	return nil
}

// createDurably writes data at path where nothing is, as writeDurably
// does, but links the finished file into place rather than renaming it,
// so a file another writer put there first is never replaced: it is
// accepted only when it holds exactly data.
func createDurably(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return errorx.Wrap(err, "write "+path, errorx.KindInvalidArgument)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(perm)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return errorx.Wrap(err, "write "+path, errorx.KindInvalidArgument)
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return errorx.Wrap(err, "write "+path, errorx.KindInvalidArgument)
		}
		if err := ownedOrAbsent(path, data); err != nil {
			return err
		}
	}
	return syncDir(dir)
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

func (p *prepared) request() (*approvalRequest, error) {
	secret := sha256.Sum256([]byte(p.Secret))
	if !p.keyed() {
		return &approvalRequest{RequestID: p.RequestID, SecretSHA256: hex.EncodeToString(secret[:])}, nil
	}
	supervisorKey, err := agent.RequestedKeyFingerprint([]byte(p.SupervisorRequestPEM))
	if err != nil {
		return nil, err
	}
	return &approvalRequest{
		RequestID: p.RequestID, SecretSHA256: hex.EncodeToString(secret[:]),
		SupervisorKeySHA256: supervisorKey.String(), AgentKeySHA256: p.AgentKeyFingerprintHex,
	}, nil
}

// enrollHost spends the approval of what prepareHost wrote: under
// standalone-enrollment.v2 it installs the supervisor's credential, and
// under v1, for a preparation holding keys, both certificates. Run
// again after an interruption it sends the same bytes: v1 installs the
// result garam recorded, and v2 the credential garam re-issues, which
// voids the one an interrupted run may have written. Once the
// enrollment record exists it installs nothing, so no replay ever
// replaces a credential renewed or re-issued since.
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
	if p.AgentDir != dirs.agent || p.AgentKeyPEM != "" {
		return nil, errorx.New(errorx.KindFailedPrecondition, "the preparation is for another agent directory, or unfinished: run `garam standalone prepare` again")
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
	client, err := standaloneClient(rootPEM, nil)
	if err != nil {
		return nil, err
	}
	if !p.keyed() {
		return enrollCredential(ctx, dirs, p, client, base)
	}
	body, err := json.Marshal(machinegen.StandaloneEnrollmentRequest{
		RequestId:                       p.RequestID,
		SupervisorCertificateRequestPem: &p.SupervisorRequestPEM,
		AgentCertificateRequestPem:      &p.AgentRequestPEM,
	})
	if err != nil {
		return nil, errorx.Wrap(err, "enrollment", errorx.KindInternal)
	}
	var result machinegen.StandaloneEnrollment
	if _, err := callStandalone(ctx, client, base+"/agent-enrollments", agent.StandaloneContractVersion,
		"Garam-Bootstrap "+p.Secret, body, &result); err != nil {
		return nil, err
	}
	if result.SupervisorCertificatePem == nil || result.AgentCertificatePem == nil || result.IssuerPem == nil {
		return nil, errorx.New(errorx.KindInternal, "garam answered a "+agent.StandaloneContractVersion+" enrollment without its certificates")
	}
	supervisorPEM := append([]byte(*result.SupervisorCertificatePem), p.SupervisorKeyPEM...)
	if _, err := tls.X509KeyPair(supervisorPEM, []byte(p.SupervisorKeyPEM)); err != nil {
		return nil, errorx.Wrap(err, "the supervisor certificate garam answered is not for the prepared key", errorx.KindInternal)
	}
	writes := []struct {
		path string
		data []byte
		perm fs.FileMode
	}{
		{filepath.Join(dirs.agent, agentCertFile), []byte(*result.AgentCertificatePem), 0o644},
		{filepath.Join(dirs.agent, issuerFile), []byte(*result.IssuerPem), 0o644},
		{filepath.Join(dirs.agent, serverRootFile), []byte(result.ServerRootPem), 0o644},
		{filepath.Join(dirs.supervisor, serverRootFile), []byte(result.ServerRootPem), 0o644},
		{filepath.Join(dirs.supervisor, supervisorFile), supervisorPEM, 0o600},
	}
	for _, w := range writes {
		if err := writeDurably(w.path, w.data, w.perm); err != nil {
			return nil, err
		}
	}
	if _, err := keyfile.NewPair(filepath.Join(dirs.agent, agentCertFile), filepath.Join(dirs.agent, agentKeyFile)); err != nil {
		return nil, errorx.Wrap(err, "the agent certificate garam answered is not for the prepared key", errorx.KindInternal)
	}
	record, err := json.Marshal(result)
	if err != nil {
		return nil, errorx.Wrap(err, "enrollment record", errorx.KindInternal)
	}
	return &result, writeDurably(filepath.Join(dirs.supervisor, enrollmentFile), record, 0o600)
}

// enrollCredential spends a v2 approval: the answer is the
// supervisor's credential and the garam server root, and nothing is
// signed. The credential is written to its own owner-only file before
// the record, and never into the record (ADR-0100).
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
	if result.Credential == nil || !supervisorCredential.MatchString(*result.Credential) ||
		result.SupervisorCertificatePem != nil {
		return nil, errorx.New(errorx.KindInternal,
			"garam answered a "+agent.CredentialStandaloneContractVersion+" enrollment without a supervisor credential")
	}
	writes := []struct {
		path string
		data []byte
		perm fs.FileMode
	}{
		{filepath.Join(dirs.supervisor, credentialFile), []byte(*result.Credential), 0o600},
		{filepath.Join(dirs.agent, serverRootFile), []byte(result.ServerRootPem), 0o644},
		{filepath.Join(dirs.supervisor, serverRootFile), []byte(result.ServerRootPem), 0o644},
	}
	for _, w := range writes {
		if err := writeDurably(w.path, w.data, w.perm); err != nil {
			return nil, err
		}
	}
	result.Credential = nil
	record, err := json.Marshal(result)
	if err != nil {
		return nil, errorx.Wrap(err, "enrollment record", errorx.KindInternal)
	}
	return &result, writeDurably(filepath.Join(dirs.supervisor, enrollmentFile), record, 0o600)
}

// supervisorCredential is a supervisor credential as api/machine.yaml
// spells one.
var supervisorCredential = regexp.MustCompile(`^garam_sc_[0-9a-f]{64}$`)

// tokenEnrolled reports an enrollment under standalone-enrollment.v2,
// whose record names no supervisor certificate.
func tokenEnrolled(e *machinegen.StandaloneEnrollment) bool {
	return e.SupervisorCertificatePem == nil
}

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

// pendingRenewal is a renewal's key and request, kept from before the
// request is sent until its answer is installed, so a retry after a
// lost answer or a restart sends the same request.
type pendingRenewal struct {
	RequestID  string `json:"requestId"`
	KeyPEM     string `json:"keyPem"`
	RequestPEM string `json:"certificateRequestPem"`
}

// renewSupervisor renews the supervisor's certificate and answers the
// new one's expiry. A pending renewal whose key is already the one in
// use was installed and is cleared; any other pending renewal is sent
// again unchanged.
func renewSupervisor(ctx context.Context, dir, machineURL string) (time.Time, error) {
	base, err := machineEndpoint(machineURL)
	if err != nil {
		return time.Time{}, err
	}
	release, err := lockDirs(dir)
	if err != nil {
		return time.Time{}, err
	}
	defer release()
	enrolled, err := readEnrollment(dir)
	if err != nil {
		return time.Time{}, errorx.Wrap(err, "run `garam standalone enroll` first", errorx.KindFailedPrecondition)
	}
	if tokenEnrolled(enrolled) {
		return time.Time{}, errorx.New(errorx.KindFailedPrecondition,
			"this supervisor enrolled under "+agent.CredentialStandaloneContractVersion+
				" and holds no certificate to renew: `garam standalone run` exchanges its credential for tokens")
	}
	current := filepath.Join(dir, supervisorFile)
	pending, err := readPending(dir)
	if err != nil {
		return time.Time{}, err
	}
	if pending != nil {
		installed, notAfter, err := installedKey(current, pending.KeyPEM)
		if err != nil {
			return time.Time{}, err
		}
		if installed {
			return notAfter, removeDurably(filepath.Join(dir, renewalFile))
		}
	} else {
		keyPEM, requestPEM, err := newKeyAndRequest()
		if err != nil {
			return time.Time{}, err
		}
		pending = &pendingRenewal{RequestID: uuid.NewString(), KeyPEM: string(keyPEM), RequestPEM: string(requestPEM)}
		raw, err := json.Marshal(pending)
		if err != nil {
			return time.Time{}, errorx.Wrap(err, "renewal", errorx.KindInternal)
		}
		if err := writeDurably(filepath.Join(dir, renewalFile), raw, 0o600); err != nil {
			return time.Time{}, err
		}
	}
	rootPEM, err := os.ReadFile(filepath.Join(dir, serverRootFile))
	if err != nil {
		return time.Time{}, errorx.Wrap(err, "server root", errorx.KindFailedPrecondition)
	}
	pair, err := keyfile.NewPair(current, current)
	if err != nil {
		return time.Time{}, err
	}
	client, err := standaloneClient(rootPEM, pair)
	if err != nil {
		return time.Time{}, err
	}
	body, err := json.Marshal(machinegen.SupervisorRenewalRequest{RequestId: pending.RequestID, CertificateRequestPem: pending.RequestPEM})
	if err != nil {
		return time.Time{}, errorx.Wrap(err, "renewal", errorx.KindInternal)
	}
	var issued machinegen.InitialCertificate
	if _, err := callStandalone(ctx, client, base+"/supervisors/self/certificate", agent.StandaloneContractVersion,
		"", body, &issued); err != nil {
		return time.Time{}, err
	}
	next := append([]byte(issued.CertificatePem), pending.KeyPEM...)
	if _, err := tls.X509KeyPair(next, []byte(pending.KeyPEM)); err != nil {
		return time.Time{}, errorx.Wrap(err, "the certificate garam answered is not for the renewal's key", errorx.KindInternal)
	}
	// One rename replaces certificate and key together; the pending
	// renewal goes only once they are in place.
	if err := writeDurably(current, next, 0o600); err != nil {
		return time.Time{}, err
	}
	return issued.NotAfter, removeDurably(filepath.Join(dir, renewalFile))
}

func readPending(dir string) (*pendingRenewal, error) {
	path := filepath.Join(dir, renewalFile)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	raw, err := keyfile.Read(path)
	if err != nil {
		return nil, err
	}
	var p pendingRenewal
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errorx.Wrap(err, "pending renewal", errorx.KindInvalidArgument)
	}
	return &p, nil
}

// installedKey reports whether the credential file at path already
// holds keyPEM, and the expiry of the certificate beside it.
func installedKey(path, keyPEM string) (bool, time.Time, error) {
	raw, err := keyfile.Read(path)
	if err != nil {
		return false, time.Time{}, err
	}
	pair, err := tls.X509KeyPair(raw, raw)
	if err != nil {
		return false, time.Time{}, errorx.Wrap(err, "supervisor credential", errorx.KindInvalidArgument)
	}
	return bytes.Contains(raw, []byte(keyPEM)), pair.Leaf.NotAfter, nil
}

// newKeyAndRequest generates an ECDSA P-256 key and a certificate
// signing request over it. garam reads the request's key alone, so the
// request names nothing.
func newKeyAndRequest() (keyPEM, requestPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "generate key", errorx.KindInternal)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "encode key", errorx.KindInternal)
	}
	request, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "certificate request", errorx.KindInternal)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: request}), nil
}

// standaloneClient reaches garam's machine listener, verifying it by
// the garam server root (ADR-0040) and presenting pair where one is
// given.
func standaloneClient(rootPEM []byte, pair *keyfile.Pair) (*http.Client, error) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		return nil, errorx.New(errorx.KindInvalidArgument, "server root: no certificate")
	}
	config := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
	if pair != nil {
		config.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return pair.Get() }
	}
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
