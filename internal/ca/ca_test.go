package ca

import (
	"context"
	"diskord/internal/securefs"
	"os"
	"os/exec"
	"testing"
)

func TestOpenSSLIssueValidateScope(t *testing.T) {
	if _, e := exec.LookPath("openssl"); e != nil {
		t.Skip("system OpenSSL unavailable")
	}
	root := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
		t.Setenv(k, os.Getenv(k))
	}
	if e := securefs.Prepare(root); e != nil {
		t.Fatal(e)
	}
	if e := Issue(context.Background(), root, "ca/root.pem", "ca/root.key", "openssl"); e != nil {
		t.Fatal(e)
	}
	a, e := Load(root, "ca/root.pem", "ca/root.key")
	if e != nil {
		t.Fatal(e)
	}
	if a.TrustedForTLS() {
		t.Fatal("newly issued test CA unexpectedly trusted by the operating system")
	}
	if _, e = a.GetCert("discord.com"); e != nil {
		t.Fatal(e)
	}
	if _, e = a.GetCert("example.com"); e == nil {
		t.Fatal("scope escaped")
	}
	if e = Issue(context.Background(), root, "ca/root.pem", "ca/root.key", "openssl"); e == nil {
		t.Fatal("overwrote existing CA")
	}
	if _, e = Load(root, "ca/root.pem", "../escape"); e == nil {
		t.Fatal("escaped runtime")
	}
}
