package keyfile

import (
	"crypto/tls"
	"os"

	"github.com/garamsh/garam/lib/errorx"
)

// Pair is a certificate and the private key it was issued with, held as
// the two files they arrive in rather than as the parsed pair. Every
// call to [Pair.Get] reads them again, so replacing the files replaces
// what the next handshake presents and no process restarts (ADR-0040).
//
// The private key goes through [Read] and the certificate beside it is
// public, so it is read plainly.
type Pair struct {
	certFile string
	keyFile  string
}

// NewPair returns the pair the two files hold, refusing what it cannot
// read now: a file that is missing, a key readable beyond its owner, or
// contents that are not a certificate and its key. The read is repeated
// on every Get, and this one is what stops a process that could never
// have served a handshake.
func NewPair(certFile, keyFile string) (*Pair, error) {
	p := &Pair{certFile: certFile, keyFile: keyFile}
	if _, err := p.Get(); err != nil {
		return nil, err
	}
	return p, nil
}

// Get returns the pair as the files hold it now. It costs two file
// reads and one parse per call, which the callers pay per handshake
// rather than per request.
func (p *Pair) Get() (*tls.Certificate, error) {
	certPEM, err := os.ReadFile(p.certFile)
	if err != nil {
		return nil, errorx.Wrap(err, "keyfile: certificate", errorx.KindInvalidArgument)
	}
	keyPEM, err := Read(p.keyFile)
	if err != nil {
		return nil, err
	}
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errorx.Wrap(err, "keyfile: certificate and key", errorx.KindInvalidArgument)
	}
	return &certificate, nil
}
