//go:build windows

package logfile

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

var logACE = regexp.MustCompile(`\(([^()]*)\)`)

func assertPrivateLog(t *testing.T, name string, _ os.FileMode) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(name, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || sd == nil {
		t.Fatalf("read log ACL: %v", err)
	}
	sddl := sd.String()
	start := strings.Index(sddl, "D:P")
	if start < 0 {
		t.Fatalf("log ACL does not block inheritance: %q", sddl)
	}
	entries := logACE.FindAllStringSubmatch(sddl[start+3:], -1)
	if len(entries) != 2 {
		t.Fatalf("expected system and current-user access only: %q", sddl)
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{"S-1-5-18": false, user.User.Sid.String(): false}
	for _, entry := range entries {
		fields := strings.Split(entry[1], ";")
		if len(fields) < 6 || fields[0] != "A" || fields[2] != "FA" {
			t.Fatalf("unexpected log ACL entry: %q", entry[1])
		}
		sid, err := windows.StringToSid(fields[5])
		if err != nil {
			t.Fatalf("invalid log ACL identity %q: %v", fields[5], err)
		}
		if _, ok := expected[sid.String()]; !ok {
			t.Fatalf("unexpected log ACL identity: %q", fields[5])
		}
		expected[sid.String()] = true
	}
	for sid, seen := range expected {
		if !seen {
			t.Fatalf("missing required log ACL identity: %s", sid)
		}
	}
}
