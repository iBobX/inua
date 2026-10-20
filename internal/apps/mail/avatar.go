package mail

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Avatar is an account's photo: kept from before, else fetched from the
// provider's address and kept. Small and an image, or refused.
func (s *Store) Avatar(ctx context.Context, acc Account) ([]byte, error) {
	path := s.AvatarPath(acc.ID)
	if b, err := os.ReadFile(path); err == nil {
		return b, nil
	}
	if !strings.HasPrefix(acc.Picture, "https://") {
		return nil, errors.New("no photo")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, acc.Picture, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		return nil, errors.New("not a photo: " + resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		writeFile(path, b)
	}
	return b, nil
}

// DisplayName is how the index calls an account: its name, else the part
// of its address before the @.
func (a Account) DisplayName() string {
	if a.Name != "" {
		return a.Name
	}
	local, _, _ := strings.Cut(a.Address, "@")
	return local
}
