package securefs

import "os"

// LockExisting locks an existing runtime without creating files or changing
// permissions. It is used only for immutable, stopped-archive readers.
func LockExisting(root string) (*Lock, error) {
	path, err := Within(root, "state/diskord.lock")
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}
