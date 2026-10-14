package keyring

import (
	"errors"
	"os"
	"sync"

	sys "golang.org/x/sys/unix"
)

// Vault keeps secrets while the keyring is unlocked, each in memory of
// its own outside Go's heap: from memfd_secret, which takes the pages out
// of the kernel's own map and never swaps them; else anonymous memory,
// locked out of swap and left out of core dumps. Wipe zeroes and unmaps
// them all.
type Vault struct {
	mu sync.Mutex
	m  map[string]region
	// plain is set once memfd_secret failed: the kernel lacks it, or it
	// is turned off.
	plain bool
}

// region is one secret's pages; n of its bytes are the secret.
type region struct {
	mem []byte
	n   int
}

// ErrVaultFull is a secret the vault could not take: past the locked
// memory limit, it is read from pass each time.
var ErrVaultFull = errors.New("no memory left to keep the secret in")

// NewVault makes an empty vault.
func NewVault() *Vault { return &Vault{m: map[string]region{}} }

// Put keeps a copy of the secret under name, replacing what was there.
func (v *Vault) Put(name string, secret []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	mem, err := v.alloc(len(secret))
	if err != nil {
		return err
	}
	copy(mem, secret)
	if old, ok := v.m[name]; ok {
		free(old.mem)
	}
	v.m[name] = region{mem: mem, n: len(secret)}
	return nil
}

// Get copies a secret out, for the caller to clear once used.
func (v *Vault) Get(name string) ([]byte, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	r, ok := v.m[name]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), r.mem[:r.n]...), true
}

// Has reports whether a secret is kept.
func (v *Vault) Has(name string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	_, ok := v.m[name]
	return ok
}

// Remove forgets a secret.
func (v *Vault) Remove(name string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if r, ok := v.m[name]; ok {
		free(r.mem)
		delete(v.m, name)
	}
}

// Len is how many secrets are kept.
func (v *Vault) Len() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.m)
}

// Wipe forgets every secret.
func (v *Vault) Wipe() {
	v.mu.Lock()
	defer v.mu.Unlock()
	for name, r := range v.m {
		free(r.mem)
		delete(v.m, name)
	}
}

// alloc maps whole pages for n bytes (at least one page).
func (v *Vault) alloc(n int) ([]byte, error) {
	page := os.Getpagesize()
	size := max(page, (n+page-1)/page*page)
	if !v.plain {
		if mem, err := secretPages(size); err == nil {
			return mem, nil
		}
		v.plain = true
	}
	mem, err := sys.Mmap(-1, 0, size, sys.PROT_READ|sys.PROT_WRITE, sys.MAP_PRIVATE|sys.MAP_ANONYMOUS)
	if err != nil {
		return nil, err
	}
	if err := sys.Mlock(mem); err != nil {
		sys.Munmap(mem)
		return nil, ErrVaultFull
	}
	sys.Madvise(mem, sys.MADV_DONTDUMP)
	return mem, nil
}

// secretPages maps size bytes from memfd_secret.
func secretPages(size int) ([]byte, error) {
	fd, err := sys.MemfdSecret(0)
	if err != nil {
		return nil, err
	}
	defer sys.Close(fd)
	if err := sys.Ftruncate(fd, int64(size)); err != nil {
		return nil, err
	}
	return sys.Mmap(fd, 0, size, sys.PROT_READ|sys.PROT_WRITE, sys.MAP_SHARED)
}

// free zeroes pages and unmaps them.
func free(mem []byte) {
	clear(mem)
	sys.Munmap(mem)
}
