package store

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"

	"diskord/internal/securefs"
)

var ErrReadOnly = errors.New("archive is read-only")

// OpenReadOnly requires the caller to hold LockExisting until after Close.
// immutable avoids even SQLite shared-memory writes. Consequently a nonempty
// WAL is rejected rather than silently exporting an older main-file snapshot.
func OpenReadOnly(root string) (*Store, error) {
	path, err := securefs.Within(root, "db/main.sqlite")
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("archive database is not a regular file")
	}
	wal, err := securefs.Within(root, "db/main.sqlite-wal")
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(wal); err == nil && st.Size() != 0 {
		return nil, errors.New("archive has a nonempty WAL; use the online API or cleanly stop diskord first")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	p := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	u.RawQuery = "mode=ro&immutable=1&_pragma=query_only(1)&_pragma=temp_store(MEMORY)"
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	var version int
	if err = db.QueryRow("SELECT version FROM schema_version").Scan(&version); err != nil || (version != 4 && version != SchemaVersion) {
		db.Close()
		return nil, errors.New("unsupported archive schema; read-only commands never migrate databases")
	}
	return &Store{Root: root, reader: db, schemaVersion: version}, nil
}
