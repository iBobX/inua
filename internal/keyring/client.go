package keyring

import (
	"context"
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// Client talks to whichever Secret Service is on the bus: gnome-keyring
// (to import from it), inua-keyring, KWallet. The assistant's keys and
// `inua keyring` use it.
type Client struct {
	conn    *dbus.Conn
	svc     dbus.BusObject
	session dbus.ObjectPath
	c       crypter
}

// ErrNoService is a bus without a Secret Service (and none to start).
var ErrNoService = errors.New("no Secret Service on the session bus")

// Connect opens a session with the Secret Service: encrypted (alg AlgDH)
// or not (AlgPlain).
func Connect(conn *dbus.Conn, alg string) (*Client, error) {
	cl := &Client{conn: conn, svc: conn.Object(Name, basePath)}
	var out dbus.Variant
	var sp dbus.ObjectPath
	switch alg {
	case AlgPlain:
		if err := cl.svc.Call(ifService+".OpenSession", 0, AlgPlain, dbus.MakeVariant("")).Store(&out, &sp); err != nil {
			return nil, serviceErr(err)
		}
		cl.c = plain{}
	case AlgDH:
		key, err := newDHKey()
		if err != nil {
			return nil, err
		}
		if err := cl.svc.Call(ifService+".OpenSession", 0, AlgDH, dbus.MakeVariant(key.pub.Bytes())).Store(&out, &sp); err != nil {
			return nil, serviceErr(err)
		}
		peer, ok := out.Value().([]byte)
		if !ok {
			return nil, errors.New("secret service: no public key in the answer")
		}
		k, err := key.aesKey(peer)
		if err != nil {
			return nil, err
		}
		cl.c = aesSession{k}
	default:
		return nil, fmt.Errorf("unknown algorithm %q", alg)
	}
	cl.session = sp
	return cl, nil
}

func serviceErr(err error) error {
	var derr dbus.Error
	if errors.As(err, &derr) && (derr.Name == "org.freedesktop.DBus.Error.ServiceUnknown" || derr.Name == "org.freedesktop.DBus.Error.NameHasNoOwner") {
		return ErrNoService
	}
	return fmt.Errorf("secret service: %w", err)
}

// Close ends the session.
func (c *Client) Close() {
	c.conn.Object(Name, c.session).Call(ifSession+".Close", 0)
}

// Ours reports whether inua-keyring is the service answering.
func (c *Client) Ours() bool {
	var a, b string
	bus := c.conn.BusObject()
	return bus.Call("org.freedesktop.DBus.GetNameOwner", 0, Name).Store(&a) == nil &&
		bus.Call("org.freedesktop.DBus.GetNameOwner", 0, OwnName).Store(&b) == nil && a == b
}

func (c *Client) prop(p dbus.ObjectPath, iface, name string, v any) error {
	variant, err := c.conn.Object(Name, p).GetProperty(iface + "." + name)
	if err != nil {
		return err
	}
	return variant.Store(v)
}

// RemoteCollection is a collection of the service.
type RemoteCollection struct {
	Path    dbus.ObjectPath
	Label   string
	Default bool // the default alias names it
}

// Collections lists the service's collections.
func (c *Client) Collections() ([]RemoteCollection, error) {
	var paths []dbus.ObjectPath
	if err := c.prop(basePath, ifService, "Collections", &paths); err != nil {
		return nil, err
	}
	var def dbus.ObjectPath
	c.svc.Call(ifService+".ReadAlias", 0, "default").Store(&def)
	var out []RemoteCollection
	for _, p := range paths {
		rc := RemoteCollection{Path: p, Default: p == def}
		c.prop(p, ifCollection, "Label", &rc.Label)
		out = append(out, rc)
	}
	return out, nil
}

// RemoteItem is an item with its secret.
type RemoteItem struct {
	Path              dbus.ObjectPath
	Label             string
	Attributes        map[string]string
	Created, Modified uint64
	Secret            []byte
	ContentType       string
}

// Items reads a collection's items with their secrets, unlocking it
// first (the service may show a prompt).
func (c *Client) Items(ctx context.Context, coll dbus.ObjectPath) ([]RemoteItem, error) {
	var paths []dbus.ObjectPath
	if err := c.prop(coll, ifCollection, "Items", &paths); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	if err := c.unlock(ctx, append([]dbus.ObjectPath{coll}, paths...)); err != nil {
		return nil, err
	}
	secrets, err := c.secrets(paths)
	if err != nil {
		return nil, err
	}
	var out []RemoteItem
	for _, p := range paths {
		it := RemoteItem{Path: p}
		c.prop(p, ifItem, "Label", &it.Label)
		c.prop(p, ifItem, "Attributes", &it.Attributes)
		c.prop(p, ifItem, "Created", &it.Created)
		c.prop(p, ifItem, "Modified", &it.Modified)
		sec, ok := secrets[p]
		if !ok {
			return nil, fmt.Errorf("%s: no secret given", p)
		}
		if it.Secret, err = c.c.open(sec.Parameters, sec.Value); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		it.ContentType = sec.ContentType
		out = append(out, it)
	}
	return out, nil
}

func (c *Client) secrets(paths []dbus.ObjectPath) (map[dbus.ObjectPath]Secret, error) {
	var out map[dbus.ObjectPath]Secret
	if err := c.svc.Call(ifService+".GetSecrets", 0, paths, c.session).Store(&out); err != nil {
		return nil, fmt.Errorf("secret service: %w", err)
	}
	return out, nil
}

// unlock unlocks objects, through the service's prompt when it shows one.
func (c *Client) unlock(ctx context.Context, objects []dbus.ObjectPath) error {
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := c.svc.Call(ifService+".Unlock", 0, objects).Store(&unlocked, &prompt); err != nil {
		return fmt.Errorf("secret service: unlock: %w", err)
	}
	if prompt == "/" || prompt == "" {
		return nil
	}
	sigs := make(chan *dbus.Signal, 4)
	c.conn.Signal(sigs)
	defer c.conn.RemoveSignal(sigs)
	match := []dbus.MatchOption{dbus.WithMatchObjectPath(prompt), dbus.WithMatchInterface("org.freedesktop.Secret.Prompt")}
	if err := c.conn.AddMatchSignal(match...); err != nil {
		return err
	}
	defer c.conn.RemoveMatchSignal(match...)
	if err := c.conn.Object(Name, prompt).Call("org.freedesktop.Secret.Prompt.Prompt", 0, "").Err; err != nil {
		return fmt.Errorf("secret service: prompt: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			c.conn.Object(Name, prompt).Call("org.freedesktop.Secret.Prompt.Dismiss", 0)
			return ctx.Err()
		case sig := <-sigs:
			if sig.Path != prompt || sig.Name != "org.freedesktop.Secret.Prompt.Completed" {
				continue
			}
			if dismissed, _ := sig.Body[0].(bool); dismissed {
				return errors.New("secret service: unlocking was dismissed")
			}
			return nil
		}
	}
}

// Search describes the items having the attributes, without their
// secrets: nothing is decrypted, nothing asks.
func (c *Client) Search(attrs map[string]string) ([]RemoteItem, error) {
	var unlocked, locked []dbus.ObjectPath
	if err := c.svc.Call(ifService+".SearchItems", 0, attrs).Store(&unlocked, &locked); err != nil {
		return nil, fmt.Errorf("secret service: %w", err)
	}
	var out []RemoteItem
	for _, p := range append(unlocked, locked...) {
		it := RemoteItem{Path: p}
		c.prop(p, ifItem, "Label", &it.Label)
		c.prop(p, ifItem, "Attributes", &it.Attributes)
		c.prop(p, ifItem, "Modified", &it.Modified)
		out = append(out, it)
	}
	return out, nil
}

// Lookup returns the secret of the first item having the attributes;
// false when there is none.
func (c *Client) Lookup(ctx context.Context, attrs map[string]string) ([]byte, bool, error) {
	var unlocked, locked []dbus.ObjectPath
	if err := c.svc.Call(ifService+".SearchItems", 0, attrs).Store(&unlocked, &locked); err != nil {
		return nil, false, fmt.Errorf("secret service: %w", err)
	}
	if len(unlocked) == 0 && len(locked) == 0 {
		return nil, false, nil
	}
	p := append(unlocked, locked...)[0]
	if len(unlocked) == 0 {
		if err := c.unlock(ctx, []dbus.ObjectPath{p}); err != nil {
			return nil, false, err
		}
	}
	secrets, err := c.secrets([]dbus.ObjectPath{p})
	if err != nil {
		return nil, false, err
	}
	sec, ok := secrets[p]
	if !ok {
		return nil, false, nil
	}
	v, err := c.c.open(sec.Parameters, sec.Value)
	return v, err == nil, err
}

// Store keeps a secret in the default collection, replacing the item with
// the same attributes.
func (c *Client) Store(ctx context.Context, label string, attrs map[string]string, secret []byte) error {
	coll := basePath + "/aliases/default"
	var def dbus.ObjectPath
	if c.svc.Call(ifService+".ReadAlias", 0, "default").Store(&def) == nil && def != "/" {
		coll = def
	}
	if err := c.unlock(ctx, []dbus.ObjectPath{coll}); err != nil {
		return err
	}
	params, value, err := c.c.seal(secret)
	if err != nil {
		return err
	}
	props := map[string]dbus.Variant{
		ifItem + ".Label":      dbus.MakeVariant(label),
		ifItem + ".Attributes": dbus.MakeVariant(attrs),
	}
	var item, prompt dbus.ObjectPath
	err = c.conn.Object(Name, coll).Call(ifCollection+".CreateItem", 0, props,
		Secret{Session: c.session, Parameters: params, Value: value, ContentType: "text/plain"}, true).Store(&item, &prompt)
	if err != nil {
		return fmt.Errorf("secret service: %w", err)
	}
	if prompt != "/" && prompt != "" {
		return errors.New("secret service: storing wants a prompt")
	}
	return nil
}

// Update gives the items having attrs new attributes and a new label,
// leaving their secrets as they are.
func (c *Client) Update(attrs, newAttrs map[string]string, label string) error {
	var unlocked, locked []dbus.ObjectPath
	if err := c.svc.Call(ifService+".SearchItems", 0, attrs).Store(&unlocked, &locked); err != nil {
		return fmt.Errorf("secret service: %w", err)
	}
	for _, p := range append(unlocked, locked...) {
		obj := c.conn.Object(Name, p)
		if err := obj.SetProperty(ifItem+".Attributes", dbus.MakeVariant(newAttrs)); err != nil {
			return fmt.Errorf("secret service: %w", err)
		}
		if err := obj.SetProperty(ifItem+".Label", dbus.MakeVariant(label)); err != nil {
			return fmt.Errorf("secret service: %w", err)
		}
	}
	return nil
}

// Delete deletes the items having the attributes.
func (c *Client) Delete(attrs map[string]string) error {
	var unlocked, locked []dbus.ObjectPath
	if err := c.svc.Call(ifService+".SearchItems", 0, attrs).Store(&unlocked, &locked); err != nil {
		return fmt.Errorf("secret service: %w", err)
	}
	for _, p := range append(unlocked, locked...) {
		var prompt dbus.ObjectPath
		if err := c.conn.Object(Name, p).Call(ifItem+".Delete", 0).Store(&prompt); err != nil {
			return fmt.Errorf("secret service: %w", err)
		}
	}
	return nil
}
