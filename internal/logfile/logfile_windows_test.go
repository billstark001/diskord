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
	allowed := map[string]bool{"SY": true, user.User.Sid.String(): true}
	for _, entry := range entries {
		fields := strings.Split(entry[1], ";")
		if len(fields) < 6 || fields[0] != "A" || fields[2] != "FA" || !allowed[fields[5]] {
			t.Fatalf("unexpected log ACL entry: %q", entry[1])
		}
	}
}
