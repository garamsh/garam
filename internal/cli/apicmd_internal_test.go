package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/garamsh/garam/internal/errorx"
)

// TestAPIOperations_PlaceEveryOperationOfTheSpecification is the
// coverage check ADR-0122 asks for: every operation api/openapi.yaml
// declares is a derived command, a hand-written one, excluded with a
// reason, or pending, and in exactly one of those. A pending operation
// derives no command yet.
func TestAPIOperations_PlaceEveryOperationOfTheSpecification(t *testing.T) {
	raw, err := os.ReadFile("../../api/openapi.yaml")
	require.NoError(t, err)
	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `yaml:"operationId"`
		} `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &spec))
	rawCfg, err := os.ReadFile("commands.yaml")
	require.NoError(t, err)
	var cfg struct {
		Exclude map[string]string `yaml:"exclude"`
		Hand    map[string]string `yaml:"hand"`
		Pending []string          `yaml:"pending"`
	}
	require.NoError(t, yaml.Unmarshal(rawCfg, &cfg))

	placed := map[string]int{}
	for _, op := range apiOperations {
		placed[op.ID]++
	}
	for id := range cfg.Exclude {
		placed[id]++
	}
	for id := range cfg.Hand {
		placed[id]++
	}
	for _, id := range cfg.Pending {
		placed[id]++
	}
	declared := 0
	for _, item := range spec.Paths {
		for method, op := range item {
			if op.OperationID == "" {
				continue
			}
			declared++
			assert.Equal(t, 1, placed[op.OperationID], "%s %s is placed once", strings.ToUpper(method), op.OperationID)
			delete(placed, op.OperationID)
		}
	}
	assert.Empty(t, placed, "every name commands.yaml places is an operation of the specification")
	assert.Equal(t, 80, declared, "the count ADR-0122 was measured at; a change here is a change the ADR's table records")
}

// signedInTo writes a credentials file signed in to endpoint under a
// fresh XDG_CONFIG_HOME, as garam login leaves one.
func signedInTo(t *testing.T, endpoint string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(string(envXDGConfigHome), dir)
	store := credentialStore{path: filepath.Join(dir, "garam", "credentials")}
	require.NoError(t, store.update(func(file *credentialsFile) (bool, error) {
		file.Endpoints[endpoint] = &endpointCredentials{
			AccessToken: "access", AccessExpiresAt: time.Now().Add(time.Hour),
			RefreshToken: "refresh", RefreshExpiresAt: time.Now().Add(24 * time.Hour), Org: "acme",
		}
		return true, nil
	}))
}

// runRoot runs one garam command and answers its stdout, its stderr
// and what it returned.
func runRoot(t *testing.T, args ...string) (string, string, *cobra.Command, error) {
	t.Helper()
	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	cmd, err := root.ExecuteC()
	return stdout.String(), stderr.String(), cmd, err
}

const issuedKey = `{"key":{"kid":"kid-1","algorithm":"RS256","state":"active","issuedAt":"2026-10-20T00:00:00Z"},` +
	`"privateKeyPem":"-----BEGIN PRIVATE KEY-----\nMIIsecretkeymaterial\n-----END PRIVATE KEY-----\n",` +
	`"clientId":"grn:acme:default:operator:hosted","tokenUrl":"https://machine.garam.test/oauth/token"}`

// keyServer answers issueOperatorKey and counts the issues it served.
func keyServer(t *testing.T) (*httptest.Server, *atomic.Int32, *[]byte) {
	t.Helper()
	var issued atomic.Int32
	var sent []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/orgs/acme/operators/hosted/keys" {
			http.NotFound(w, r)
			return
		}
		sent, _ = io.ReadAll(r.Body)
		issued.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(issuedKey))
	}))
	t.Cleanup(srv.Close)
	return srv, &issued, &sent
}

// TestOperatorsKeysIssue_WritesTheKeyToAFileAndNoSecretToStdout: the
// answer reaches the file --out names, created at mode 0600, and
// stdout carries its kid and no key material, in either output form.
// The file's contents are the control that the key was answered at
// all, so its absence from stdout is the redaction's doing.
func TestOperatorsKeysIssue_WritesTheKeyToAFileAndNoSecretToStdout(t *testing.T) {
	srv, issued, sent := keyServer(t)
	signedInTo(t, srv.URL)
	for _, output := range []string{"table", "json"} {
		t.Run(output, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "key.json")
			stdout, stderr, _, err := runRoot(t, "operators", "keys", "issue", "hosted",
				"--rotation", "planned", "--out", out, "--endpoint", srv.URL, "-o", output)
			require.NoError(t, err, stderr)

			info, err := os.Stat(out)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			written, err := os.ReadFile(out)
			require.NoError(t, err)
			assert.Contains(t, string(written), "BEGIN PRIVATE KEY", "the file holds the key")
			assert.JSONEq(t, `{"rotation":"planned"}`, string(*sent))

			assert.Contains(t, stdout, "kid-1")
			for _, secret := range []string{"PRIVATE KEY", "MIIsecretkeymaterial", "privateKeyPem"} {
				assert.NotContains(t, stdout, secret)
				assert.NotContains(t, stderr, secret)
			}
			assert.Contains(t, stderr, out)
		})
	}
	assert.EqualValues(t, 2, issued.Load())
}

// TestOperatorsKeysIssue_RefusesWithoutAFileOrOverAnExistingOne: no
// --out sends nothing, and an existing file sends nothing either and
// is kept, so a key is never issued into nowhere: with --rotation
// immediate that would leave the operator keyless. The successful issue
// above is the control that the server is reachable.
func TestOperatorsKeysIssue_RefusesWithoutAFileOrOverAnExistingOne(t *testing.T) {
	srv, issued, _ := keyServer(t)
	signedInTo(t, srv.URL)

	_, _, cmd, err := runRoot(t, "operators", "keys", "issue", "hosted", "--rotation", "planned", "--endpoint", srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--out")
	assert.Equal(t, ExitInvalid, ExitCode(cmd, err))
	assert.EqualValues(t, 0, issued.Load(), "nothing is issued without a file to put it in")

	out := filepath.Join(t.TempDir(), "key.json")
	require.NoError(t, os.WriteFile(out, []byte("earlier"), 0o600))
	_, _, cmd, err = runRoot(t, "operators", "keys", "issue", "hosted", "--rotation", "planned", "--out", out, "--endpoint", srv.URL)
	require.Error(t, err)
	assert.Equal(t, ExitConflict, ExitCode(cmd, err))
	assert.EqualValues(t, 0, issued.Load(), "nothing is issued while the file it would go to exists")
	kept, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	assert.Equal(t, "earlier", string(kept), "an existing file is never overwritten")
}

// TestOperatorsKeysIssue_LeavesNoFileWhereNoKeyWasIssued: the file is
// created before the request and removed when garam refuses or cannot
// be reached, so what --out names exists only holding an answer. The
// issue that succeeds, above, is the control that the file is kept.
func TestOperatorsKeysIssue_LeavesNoFileWhereNoKeyWasIssued(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"kind":"failed_precondition","message":"no token endpoint is configured","reason":"key_issuance_unconfigured"}`))
	}))
	t.Cleanup(refusing.Close)
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()

	for name, endpoint := range map[string]string{"refused": refusing.URL, "unreachable": goneURL} {
		t.Run(name, func(t *testing.T) {
			signedInTo(t, endpoint)
			out := filepath.Join(t.TempDir(), "key.json")
			_, _, _, err := runRoot(t, "operators", "keys", "issue", "hosted", "--rotation", "planned", "--out", out, "--endpoint", endpoint)
			require.Error(t, err)
			_, statErr := os.Stat(out)
			assert.True(t, errors.Is(statErr, os.ErrNotExist), "no file is left where no key was issued: %v", statErr)
		})
	}
}

// TestAPICommand_ReadsAWrappedListAsRows: listOperatorKeys answers an
// object holding the list, and the table's rows are the list's items.
func TestAPICommand_ReadsAWrappedListAsRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/orgs/acme/operators/hosted/keys", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[{"kid":"kid-1","algorithm":"RS256","state":"active","issuedAt":"2026-10-20T00:00:00Z"},` +
			`{"kid":"kid-2","algorithm":"RS256","state":"revoked","issuedAt":"2026-10-19T00:00:00Z"}]}`))
	}))
	t.Cleanup(srv.Close)
	signedInTo(t, srv.URL)

	stdout, stderr, _, err := runRoot(t, "operators", "keys", "list", "hosted", "--endpoint", srv.URL)
	require.NoError(t, err, stderr)
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 3, stdout)
	assert.Regexp(t, `^KID\s+ALGORITHM\s+STATE`, lines[0])
	assert.Regexp(t, `^kid-1\s+RS256\s+active`, lines[1])
	assert.Regexp(t, `^kid-2\s+RS256\s+revoked`, lines[2])

	stdout, _, _, err = runRoot(t, "operators", "keys", "list", "hosted", "--endpoint", srv.URL, "-o", "json")
	require.NoError(t, err)
	var doc map[string][]map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "-o json is the API's own answer")
	assert.Len(t, doc["keys"], 2)
}

// TestExitCode_ReportsAUserCommandsFailureByKind, and a server
// command's by 1 as before.
func TestExitCode_ReportsAUserCommandsFailureByKind(t *testing.T) {
	user := &cobra.Command{Annotations: map[string]string{userCommandAnnotation: "true"}}
	for kind, want := range map[errorx.Kind]int{
		errorx.KindUnauthenticated:    ExitUnauthenticated,
		errorx.KindPermissionDenied:   ExitPermissionDenied,
		errorx.KindNotFound:           ExitNotFound,
		errorx.KindAlreadyExists:      ExitConflict,
		errorx.KindFailedPrecondition: ExitConflict,
		errorx.KindInvalidArgument:    ExitInvalid,
		errorx.KindUnavailable:        ExitUnavailable,
		errorx.KindInternal:           ExitFailure,
	} {
		assert.Equal(t, want, ExitCode(user, errorx.New(kind, "x")), kind.String())
	}
	assert.Equal(t, ExitUsage, ExitCode(user, errors.New("unknown flag: --nope")), "cobra's own refusal is a usage error")
	assert.Equal(t, 0, ExitCode(user, nil))
	assert.Equal(t, ExitFailure, ExitCode(&cobra.Command{}, errorx.New(errorx.KindNotFound, "x")), "a server command ends with 1")
}

// TestAPICommand_TakesAnArgumentBeginningWithADashAfterTheSeparator: a
// kid is base64url and may begin with "-". After "--" it is the
// argument it is, which is what the command's help says; without it
// cobra reads it as a flag and refuses, which is the control.
func TestAPICommand_TakesAnArgumentBeginningWithADashAfterTheSeparator(t *testing.T) {
	var revoked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		revoked = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	signedInTo(t, srv.URL)

	_, _, cmd, err := runRoot(t, "operators", "keys", "revoke", "hosted", "-plRY5S", "--endpoint", srv.URL)
	require.Error(t, err)
	assert.Equal(t, ExitUsage, ExitCode(cmd, err))
	assert.Empty(t, revoked, "nothing is sent")

	_, stderr, _, err := runRoot(t, "operators", "keys", "revoke", "--endpoint", srv.URL, "--", "hosted", "-plRY5S")
	require.NoError(t, err, stderr)
	assert.Equal(t, "/api/orgs/acme/operators/hosted/keys/-plRY5S", revoked)
}

// TestAPICommand_UnderAHandWrittenNounReadsItsFlags: orgs is a
// hand-written noun (it holds orgs use), and the derived orgs get
// beside it reads the --endpoint and --org that noun binds. Answered at
// the endpoint named is the property; a command reading its own unbound
// flags would have gone to the default endpoint instead.
func TestAPICommand_UnderAHandWrittenNounReadsItsFlags(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"grn":"grn:root:default:org:globex","name":"Globex","createdAt":"2026-10-20T00:00:00Z"}`))
	}))
	t.Cleanup(srv.Close)
	signedInTo(t, srv.URL)

	stdout, stderr, _, err := runRoot(t, "orgs", "get", "globex", "--endpoint", srv.URL)
	require.NoError(t, err, stderr)
	assert.Equal(t, "/api/orgs/globex", asked)
	assert.Regexp(t, `(?m)^NAME\s+Globex$`, stdout)
}

// TestInvitationsCreate_WritesTheTokenToAFileAlone: an invitation's
// token is a credential (ADR-0122), so it reaches the file --out names
// and no output; the role it grants is the control that the answer was
// printed at all.
func TestInvitationsCreate_WritesTheTokenToAFileAlone(t *testing.T) {
	const token = "inv_9f2ac1b40d8e7a35c0ffee"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/orgs/acme/invitations", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"` + token + `","orgGrn":"grn:root:default:org:acme","role":"member",` +
			`"expiresAt":"2026-10-27T00:00:00Z","createdAt":"2026-10-20T00:00:00Z"}`))
	}))
	t.Cleanup(srv.Close)
	signedInTo(t, srv.URL)
	out := filepath.Join(t.TempDir(), "invitation.json")

	stdout, stderr, _, err := runRoot(t, "invitations", "create", "--role", "member", "--out", out, "--endpoint", srv.URL, "-o", "json")
	require.NoError(t, err, stderr)
	written, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(written), token)
	assert.Contains(t, stdout, `"role": "member"`)
	assert.NotContains(t, stdout, token)
	assert.NotContains(t, stderr, token)
}

// TestInvitationsGet_SendsNoSession: reading an invitation asks for no
// session (security: [] in the specification), so the command sends
// none and runs signed out. orgs get beside it, which does, is the
// control that the session would have been sent.
func TestInvitationsGet_SendsNoSession(t *testing.T) {
	presented := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented[r.URL.Path] = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"org":"grn:root:default:org:acme","orgName":"Acme","role":"member","state":"pending","expiresAt":"2026-10-27T00:00:00Z"}`))
	}))
	t.Cleanup(srv.Close)
	signedInTo(t, srv.URL)

	_, stderr, _, err := runRoot(t, "invitations", "get", "--endpoint", srv.URL, "--", "inv_token")
	require.NoError(t, err, stderr)
	_, _, _, err = runRoot(t, "orgs", "get", "acme", "--endpoint", srv.URL)
	require.NoError(t, err)
	assert.Empty(t, presented["/api/invitations/inv_token"], "no session is sent where none is asked for")
	assert.Equal(t, "Bearer access", presented["/api/orgs/acme"])
}
