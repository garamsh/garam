package standalone_test

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/stretchr/testify/require"
)

// A model turn's outcome is read from where the runtime records it, not
// from whether the model chose to call message_send (#1207): garam
// records the message completed once the runtime committed the turn
// the message began, and Sherlock stores what the turn answered as its
// own sources, both what a message_send carried and the reply the turn
// ended with (internal/agent/service.go at v0.8.0). A reply is asserted
// only where it is the scenario's claim.

// awaitCompleted waits until garam records the sender's message under
// key completed, which the adapter does only once the runtime has
// committed the turn the message began, and answers the message.
func (s *stack) awaitCompleted(sender, key string) message {
	s.t.Helper()
	var got []message
	s.eventually("the turn of "+key+" completed", func() bool {
		got = s.messages("sender_grn = $1 AND idempotency_key = $2", sender, key)
		return len(got) == 1 && got[0].Completed
	})
	return got[0]
}

// observeReply claims the sender's messages for up to within and logs
// whether a reply from the agent arrived. It is a labelled observation
// and asserts nothing: whether a turn calls message_send is the model's
// choice.
func (p *sender) observeReply(from string, within time.Duration) {
	p.s.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if r := p.receive(); r != nil && r.Sender == from {
			p.s.t.Logf("reply arrived: yes: %q", p.s.scrub(r.Body))
			return
		}
	}
	p.s.t.Logf("reply arrived: no, within %s", within)
}

// store opens a copy of the runtime's store on volume, its generation
// stopped.
func (s *stack) store(volume string) *sql.DB {
	s.t.Helper()
	dir := s.t.TempDir()
	for _, name := range []string{"memory.db", "memory.db-wal"} {
		cmd := exec.Command("docker", "run", "--rm", "--network", "none", "-v", volume+":/v:ro",
			"--entrypoint", "sh", required(s.t, "GARAM_TEST_WORKSPACE_IMAGE"), "-c", "test ! -e /v/"+name+" || cat /v/"+name)
		cmd.Env = append(os.Environ(), "DOCKER_HOST="+s.daemon)
		raw, err := cmd.Output()
		require.NoError(s.t, err)
		if len(raw) > 0 {
			require.NoError(s.t, os.WriteFile(filepath.Join(dir, name), raw, 0o600))
		}
	}
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(dir, "memory.db"))
	require.NoError(s.t, err)
	s.t.Cleanup(func() { _ = db.Close() })
	return db
}

// storeStrings answers one text column of every row a query returns.
func (s *stack) storeStrings(db *sql.DB, query string, args ...any) []string {
	s.t.Helper()
	rows, err := db.Query(query, args...)
	require.NoError(s.t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var c string
		require.NoError(s.t, rows.Scan(&c))
		out = append(out, c)
	}
	require.NoError(s.t, rows.Err())
	return out
}

// answers reads the runtime's store on volume, its generation stopped,
// and answers what the runtime recorded as its own for the turn the
// message id began: the assistant sources after that message's source
// and before the next message's.
func (s *stack) answers(volume, id string) []string {
	s.t.Helper()
	return s.storeStrings(s.store(volume), `
		SELECT content FROM conversation_sources
		 WHERE role = 'assistant'
		   AND id > (SELECT id FROM conversation_sources WHERE external_id = ?1)
		   AND id < COALESCE((SELECT MIN(id) FROM conversation_sources
		                       WHERE role = 'user'
		                         AND id > (SELECT id FROM conversation_sources WHERE external_id = ?1)),
		                     9223372036854775807)
		 ORDER BY id`, id)
}
