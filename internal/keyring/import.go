package keyring

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
)

// Imported counts what Import did.
type Imported struct {
	Added, Updated, Same int
	Collections          []string // the local collections written to
}

// Import copies every item of the Secret Service c talks to (gnome-keyring)
// into the store: the default collection into login, the others into
// collections of the same label. An item already there (same collection,
// label and attributes) gets the secret if it differs. The session
// collection is left out.
func Import(ctx context.Context, c *Client, s *Store) (Imported, error) {
	var rep Imported
	if c.Ours() {
		return rep, errors.New("the Secret Service is inua-keyring already: nothing to import")
	}
	cols, err := c.Collections()
	if err != nil {
		return rep, err
	}
	for _, rc := range cols {
		if strings.HasSuffix(string(rc.Path), "/session") {
			continue
		}
		items, err := c.Items(ctx, rc.Path)
		if err != nil {
			return rep, fmt.Errorf("%s: %w", rc.Label, err)
		}
		local := Login
		if !rc.Default {
			local = ""
			for _, lc := range s.Collections() {
				if lc.Label == rc.Label && lc.ID != Session {
					local = lc.ID
				}
			}
			if local == "" {
				lc, err := s.CreateCollection(rc.Label, "")
				if err != nil {
					return rep, err
				}
				local = lc.ID
			}
		}
		rep.Collections = append(rep.Collections, local)
		for _, it := range items {
			var existing *Item
			have, _ := s.Items(local)
			for i := range have {
				if have[i].Label == it.Label && maps.Equal(have[i].Attributes, it.Attributes) {
					existing = &have[i]
					break
				}
			}
			if existing == nil {
				if _, _, err := s.CreateItem(local, it.Label, it.Attributes, it.Secret, it.ContentType, false); err != nil {
					return rep, fmt.Errorf("%s: %w", it.Label, err)
				}
				rep.Added++
				continue
			}
			if old, _, err := s.Secret(local, existing.ID, ""); err == nil && bytes.Equal(old, it.Secret) {
				rep.Same++
				continue
			}
			if err := s.SetSecret(local, existing.ID, it.Secret, it.ContentType); err != nil {
				return rep, fmt.Errorf("%s: %w", it.Label, err)
			}
			rep.Updated++
		}
	}
	return rep, nil
}
