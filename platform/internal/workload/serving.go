package workload

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/keyfile"
)

// ControlName is the name the supervisor's serving certificate carries
// and the adapter reaches control under, whatever socket carries it.
const ControlName = "supervisor"

// servingLifetime bounds the serving certificate. A generation's root
// and certificate are removed with the generation, so the bound only
// has to outlast a generation.
const servingLifetime = 10 * 365 * 24 * time.Hour

// The files of one generation's directory, which only the supervisor
// reads, but for the socket's directory, which only that generation's
// adapter mounts.
const (
	servingFile    = "serving.pem"
	controlRootPEM = "control-root.pem"
	socketDir      = "control"
	socketName     = "control.sock"
)

// generationDir is where one intent's serving material and socket
// live.
func generationDir(dir, intentID string) string {
	return filepath.Join(dir, "generations", intentID)
}

// servingMaterial answers the generation's serving certificate and the
// root the adapter verifies it by, minting both the first time. The
// root's key signs the certificate and is discarded, so nothing can
// sign another certificate this adapter would accept.
func servingMaterial(genDir string) (*tls.Certificate, []byte, error) {
	servingPath := filepath.Join(genDir, servingFile)
	rootPath := filepath.Join(genDir, controlRootPEM)
	if raw, err := keyfile.Read(servingPath); err == nil {
		cert, err := tls.X509KeyPair(raw, raw)
		if err != nil {
			return nil, nil, errorx.Wrap(err, "workload: serving certificate", errorx.KindDataLoss)
		}
		root, err := os.ReadFile(rootPath)
		if err != nil {
			return nil, nil, errorx.Wrap(err, "workload: control root", errorx.KindDataLoss)
		}
		return &cert, root, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	if err := os.MkdirAll(genDir, 0o700); err != nil {
		return nil, nil, errorx.Wrap(err, "workload: generation directory", errorx.KindUnavailable)
	}
	rootPEM, servingPEM, err := mintServing()
	if err != nil {
		return nil, nil, err
	}
	// The root goes first: a serving file is what says the material is
	// complete.
	if err := writeDurably(rootPath, rootPEM, 0o600); err != nil {
		return nil, nil, err
	}
	if err := writeDurably(servingPath, servingPEM, 0o600); err != nil {
		return nil, nil, err
	}
	cert, err := tls.X509KeyPair(servingPEM, servingPEM)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "workload: serving certificate", errorx.KindInternal)
	}
	return &cert, rootPEM, nil
}

// mintServing answers a root certificate and a serving certificate it
// signed, with that certificate's key, all in PEM form.
func mintServing() (rootPEM, servingPEM []byte, err error) {
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "workload: control root key", errorx.KindInternal)
	}
	now := time.Now()
	root := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "garam standalone control root"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(servingLifetime),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "workload: control root", errorx.KindInternal)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "workload: serving key", errorx.KindInternal)
	}
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: ControlName},
		DNSNames:     []string{ControlName},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(servingLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, root, &key.PublicKey, rootKey)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "workload: serving certificate", errorx.KindInternal)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "workload: serving key", errorx.KindInternal)
	}
	rootPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})
	servingPEM = append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})...)
	return rootPEM, servingPEM, nil
}
