package ca

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"diskord/internal/scope"
	"diskord/internal/securefs"
)

type Authority struct {
	Root   *x509.Certificate
	signer crypto.Signer
	mu     sync.Mutex
	leaves map[string]*tls.Certificate
}

func Load(root, certPath, keyPath string) (*Authority, error) {
	cp, e := securefs.Within(root, certPath)
	if e != nil {
		return nil, e
	}
	kp, e := securefs.Within(root, keyPath)
	if e != nil {
		return nil, e
	}
	if cp == kp {
		return nil, errors.New("CA certificate and private key must be separate files")
	}
	for _, p := range []string{cp, kp} {
		st, e := os.Stat(p)
		if e != nil {
			return nil, e
		}
		if !st.Mode().IsRegular() || st.Size() > 1024*1024 {
			return nil, errors.New("invalid CA file")
		}
	}
	pair, e := tls.LoadX509KeyPair(cp, kp)
	if e != nil {
		return nil, errors.New("CA PEM invalid, encrypted key unsupported, or certificate/key mismatch")
	}
	if len(pair.Certificate) != 1 {
		return nil, errors.New("select a single self-signed root CA, not a leaf or chain bundle")
	}
	cert, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil {
		return nil, e
	}
	if !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, errors.New("certificate lacks CA:true / keyCertSign")
	}
	if e = cert.CheckSignatureFrom(cert); e != nil {
		return nil, errors.New("CA must be self-signed")
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
		return nil, errors.New("CA is expired or not yet valid")
	}
	signer, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("unsupported private key type")
	}
	pub, e := x509.MarshalPKIXPublicKey(signer.Public())
	if e != nil {
		return nil, e
	}
	if !bytes.Equal(pub, cert.RawSubjectPublicKeyInfo) {
		return nil, errors.New("certificate/key mismatch")
	}
	switch key := signer.Public().(type) {
	case *rsa.PublicKey:
		if key.N.BitLen() < 2048 {
			return nil, errors.New("RSA root key must be at least 2048 bits")
		}
	case *ecdsa.PublicKey:
		if key.Curve.Params().BitSize < 256 {
			return nil, errors.New("EC root key too small")
		}
	}
	return &Authority{Root: cert, signer: signer, leaves: make(map[string]*tls.Certificate)}, nil
}
func (a *Authority) GetRootCA() *x509.Certificate { return a.Root }

// TrustedForTLS checks the operating system's current trust decision with an
// in-memory leaf. It does not modify the trust store or write a certificate.
func (a *Authority) TrustedForTLS() bool {
	pair, err := a.GetCert("discord.com")
	if err != nil {
		return false
	}
	_, err = pair.Leaf.Verify(x509.VerifyOptions{DNSName: "discord.com", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	return err == nil
}
func TrustInstructions() string {
	switch runtime.GOOS {
	case "darwin":
		return "根 CA 尚未获得本机 SSL 信任。核对 SHA-256 指纹后，只将公有证书导入当前用户的登录钥匙串，并在“钥匙串访问”中为该证书设置 SSL 信任。取消信任时，按指纹找到该证书并删除或恢复默认信任。不要导入私钥；完成后重新启动客户端。"
	case "windows":
		return "根 CA 尚未获得本机 SSL 信任。核对 SHA-256 指纹后，在 certmgr.msc 的“当前用户 → 受信任的根证书颁发机构 → 证书”导入公有证书。取消信任时，按指纹在同一位置删除证书。不要导入私钥；完成后重新启动客户端。"
	default:
		return "根 CA 尚未获得本机 SSL 信任。核对 SHA-256 指纹后，在系统证书管理器中仅信任公有证书；取消信任时按指纹删除该证书。不要导入私钥；完成后重新启动客户端。"
	}
}
func (a *Authority) Fingerprint() string {
	v := sha256.Sum256(a.Root.Raw)
	return hex.EncodeToString(v[:])
}
func (a *Authority) GetCert(host string) (*tls.Certificate, error) {
	host = scope.Host(host)
	if !scope.API(host) && !scope.Gateway(host) && !scope.CDN(host) {
		return nil, errors.New("leaf certificate requested for an out-of-scope host")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if cached := a.leaves[host]; cached != nil && time.Now().Add(time.Hour).Before(cached.Leaf.NotAfter) {
		return cached, nil
	}
	if !time.Now().Before(a.Root.NotAfter) {
		return nil, errors.New("root CA expired")
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return nil, e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return nil, e
	}
	until := time.Now().Add(24 * time.Hour)
	if a.Root.NotAfter.Before(until) {
		until = a.Root.NotAfter
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: time.Now().Add(-5 * time.Minute), NotAfter: until, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, a.Root, key.Public(), a.signer)
	if e != nil {
		return nil, e
	}
	leaf, e := x509.ParseCertificate(der)
	if e != nil {
		return nil, e
	}
	pair := &tls.Certificate{Certificate: [][]byte{der, a.Root.Raw}, PrivateKey: key, Leaf: leaf}
	if len(a.leaves) >= 128 {
		a.leaves = make(map[string]*tls.Certificate)
	}
	a.leaves[host] = pair
	return pair, nil
}

type Live struct{ value atomic.Pointer[Authority] }

func NewLive(a *Authority) *Live                           { l := &Live{}; l.value.Store(a); return l }
func (l *Live) Swap(a *Authority)                          { l.value.Store(a) }
func (l *Live) Current() *Authority                        { return l.value.Load() }
func (l *Live) GetRootCA() *x509.Certificate               { return l.Current().GetRootCA() }
func (l *Live) GetCert(h string) (*tls.Certificate, error) { return l.Current().GetCert(h) }

func Issue(ctx context.Context, root, certPath, keyPath, tool string) error {
	if certPath == "" || keyPath == "" {
		return errors.New("explicit --cert and --key paths are required")
	}
	cp, e := securefs.Within(root, certPath)
	if e != nil {
		return e
	}
	kp, e := securefs.Within(root, keyPath)
	if e != nil {
		return e
	}
	if cp == kp {
		return errors.New("certificate and key paths must differ")
	}
	bin, e := exec.LookPath(tool)
	if e != nil {
		return errors.New("OpenSSL not found; install the system toolchain or set ca.openssl to an explicit executable")
	}
	for _, p := range []string{cp, kp} {
		if _, e = os.Lstat(p); e == nil {
			return errors.New("refusing to overwrite an existing CA file")
		}
		if !os.IsNotExist(e) {
			return e
		}
		rel, e := filepath.Rel(root, filepath.Dir(p))
		if e != nil {
			return e
		}
		if _, e = securefs.Dir(root, rel); e != nil {
			return e
		}
	}
	files := []string{}
	success := false
	defer func() {
		if !success {
			for _, p := range files {
				os.Remove(p)
			}
		}
	}()
	for _, p := range []string{kp, cp} {
		f, e := securefs.NewFile(p)
		if e != nil {
			return e
		}
		f.Close()
		files = append(files, p)
	}
	nonce, e := securefs.Random(16)
	if e != nil {
		return e
	}
	conf := filepath.Join(root, "tmp", "issue-"+nonce+".cnf")
	f, e := securefs.NewFile(conf)
	if e != nil {
		return e
	}
	defer os.Remove(conf)
	_, e = f.WriteString("[req]\nprompt = no\ndistinguished_name = dn\nx509_extensions = v3_ca\n[dn]\nCN = diskord local CA\nO = diskord\n[v3_ca]\nbasicConstraints = critical,CA:TRUE,pathlen:0\nkeyUsage = critical,keyCertSign,cRLSign\nsubjectKeyIdentifier = hash\nauthorityKeyIdentifier = keyid:always\n")
	f.Close()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "req", "-new", "-x509", "-newkey", "rsa:3072", "-nodes", "-sha256", "-days", "3650", "-set_serial", "0x"+nonce, "-keyout", kp, "-out", cp, "-config", conf, "-extensions", "v3_ca", "-batch")
	cmd.Dir = root
	cmd.Env = os.Environ()
	// Do not emit arbitrary tool output or private key material into application logs.
	if e = cmd.Run(); e != nil {
		return fmt.Errorf("OpenSSL CA issuance failed; require a compatible openssl req toolchain: %w", e)
	}
	for _, p := range []string{cp, kp} {
		if e = securefs.Private(p); e != nil {
			return e
		}
	}
	if _, e = Load(root, certPath, keyPath); e != nil {
		return e
	}
	success = true
	return nil
}
