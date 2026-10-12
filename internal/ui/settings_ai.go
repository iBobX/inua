package ui

import (
	"fmt"
	"log"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
)

// The AI page: the assistant's switches, which profile answers, and a
// card for each profile (Mister Morph's llm.profiles) with its fallback
// switch (the main loop's route). Keys typed here go to internal/assistant/
// keys (the Secret Service, else a file), never to config.toml. Saving one
// may wait for the keyring's prompt, so it runs off the main loop.

// aiSettings are the AI page's controls.
type aiSettings struct {
	enable, ask, web, files, bash gtk.Switch
	profile                       gtk.DropDown
	route                         gtk.Label
	list                          gtk.Box
	keysNote                      gtk.Label
	cards                         []*aiCard
	names                         []string // the profile drop-down's

	keys     *keys.Keys
	saved    map[string]uint64 // profiles with a saved key
	savedErr error
	renames  [][2]string // profile renames whose keys move when the settings are saved
}

// aiCard is one profile's card, kept while the profiles keep their count
// and order, so typing in it keeps the caret.
type aiCard struct {
	title, state                    gtk.Label
	name, model, endpoint, env, key *gtk.Entry
	provider, reasoning             *gtk.DropDown
	fallback                        *gtk.Switch
	fallbackRow                     *gtk.Box
	up, down, remove, save, forget  *gtk.Button
}

// reasoningNames are config.ReasoningEfforts as the drop-down shows them.
var reasoningNames = []string{"Model default", "Minimal", "Low", "Medium", "High"}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (s *shellUI) getAISettings(ui *gtkx.Builder) {
	a := &s.settings.ai
	ui.Get("ai_enable", &a.enable)
	ui.Get("ai_ask", &a.ask)
	ui.Get("ai_web", &a.web)
	ui.Get("ai_files", &a.files)
	ui.Get("ai_bash", &a.bash)
	ui.Get("ai_profile", &a.profile)
	ui.Get("ai_route", &a.route)
	ui.Get("ai_profiles", &a.list)
	ui.Get("ai_keys_note", &a.keysNote)
	var add gtk.Button
	ui.Get("ai_add", &add)
	add.ConnectClicked(&s.onSettingsButton)
	for _, sw := range []*gtk.Switch{&a.enable, &a.ask, &a.web, &a.files, &a.bash} {
		sw.ConnectStateSet(&s.onSettingsSwitch)
	}
	s.settings.drops[a.profile.GoPointer()] = &a.profile
	gtkx.ConnectNotify(&a.profile, "selected", &s.onSettingsDrop)
	a.cards, a.names = nil, nil
	if a.keys == nil {
		a.keys = keys.New()
	}
	a.keysNote.SetText("Keys typed here are kept in " + a.keys.Where())
	a.refreshSaved()
}

// refreshSaved reads which profiles have a saved key; nothing is
// decrypted, so nothing asks.
func (a *aiSettings) refreshSaved() {
	a.saved, a.savedErr = a.keys.Saved()
	if a.savedErr != nil {
		log.Printf("settings: keys: %v", a.savedErr)
	}
}

// keysDo runs a change to the keys off the main loop, then shows the page
// again; failing says why.
func (s *shellUI) keysDo(what string, f func(*keys.Keys) error) {
	k := s.settings.ai.keys
	go func() {
		err := f(k)
		gtkx.Post(func() {
			if err != nil {
				s.setStatus(what+": "+err.Error(), true)
			}
			if s.settings.win != nil {
				s.settings.ai.refreshSaved()
				s.showSettings()
			}
		})
	}()
}

// applyKeyRenames moves the keys of renamed profiles, once the names
// stopped changing (the settings are saved).
func (s *shellUI) applyKeyRenames() {
	a := &s.settings.ai
	renames := a.renames
	a.renames = nil
	for _, r := range renames {
		if r[0] != r[1] && a.keys != nil {
			s.keysDo("key not moved", func(k *keys.Keys) error { return k.Rename(r[0], r[1]) })
		}
	}
}

// showAISettings sets the AI page from the settings.
func (s *shellUI) showAISettings() {
	st := &s.settings
	a := &st.ai
	c := s.cfg.Assistant
	a.enable.SetActive(c.Enable)
	a.ask.SetActive(c.AskBeforeChanges)
	a.web.SetActive(slices.Contains(c.Tools, "web_search"))
	a.files.SetActive(slices.Contains(c.Tools, "read_file"))
	a.bash.SetActive(slices.Contains(c.Tools, "bash"))

	names := make([]string, len(c.Profiles))
	for i, p := range c.Profiles {
		names[i] = p.Name
	}
	if !slices.Equal(names, a.names) {
		list := gtk.NewStringList(names)
		a.profile.SetModel(list)
		list.Unref()
		a.names = names
	}
	a.profile.SetSelected(uint32(max(0, slices.Index(names, c.Profile))))
	if len(c.Fallback) > 0 {
		a.route.SetText("Then, when it fails: " + strings.Join(c.Fallback, ", "))
	} else {
		a.route.SetText("The profile asked first; turn on Fallback on others to try them when it fails")
	}

	if len(a.cards) != len(c.Profiles) {
		s.buildAICards(len(c.Profiles))
	}
	for i, p := range c.Profiles {
		s.fillAICard(a.cards[i], i, p, a.saved, a.savedErr)
	}
}

// buildAICards makes n empty cards; fillAICard sets them.
func (s *shellUI) buildAICards(n int) {
	st := &s.settings
	a := &st.ai
	for _, card := range a.cards { // their widgets go with the box's children
		for _, e := range []*gtk.Entry{card.name, card.model, card.endpoint, card.env, card.key} {
			delete(st.entries, e.GoPointer())
		}
		delete(st.drops, card.provider.GoPointer())
		delete(st.drops, card.reasoning.GoPointer())
	}
	gtkx.Clear(&a.list)
	a.cards = nil
	providers := make([]string, len(config.AIProviders))
	for i, p := range config.AIProviders {
		providers[i] = p.Name
	}
	for i := range n {
		card := &aiCard{}
		box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
		box.AddCssClass("ai-card")

		head := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		texts := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		texts.SetHexpand(true)
		card.title = *gtk.NewLabel("")
		card.title.AddCssClass("title")
		card.title.SetXalign(0)
		card.state = *gtk.NewLabel("")
		card.state.AddCssClass("sub")
		card.state.SetXalign(0)
		card.state.SetWrap(true)
		texts.Append(&card.title.Widget)
		texts.Append(&card.state.Widget)
		gtkx.Append(head, &texts.Widget)
		button := func(action, icon, tip string) *gtk.Button {
			b := gtk.NewButtonFromIconName(icon)
			b.AddCssClass("plain")
			b.SetName(fmt.Sprintf("ai-%s:%d", action, i))
			b.SetTooltipText(tip)
			b.SetValign(gtk.AlignCenterValue)
			b.ConnectClicked(&s.onSettingsButton)
			head.Append(&b.Widget)
			return b // unreferenced with the card's other widgets
		}
		card.up = button("up", "pan-up-symbolic", "Move up (fallbacks are tried in this order)")
		card.down = button("down", "pan-down-symbolic", "Move down")
		card.remove = button("remove", "window-close-symbolic", "Remove this profile")
		gtkx.Append(box, &head.Widget)

		grid := gtk.NewGrid()
		grid.SetColumnSpacing(12)
		grid.SetRowSpacing(8)
		grid.SetColumnHomogeneous(true)
		entry := func(field, placeholder string) *gtk.Entry {
			e := gtk.NewEntry()
			e.SetName(fmt.Sprintf("ai-%s:%d", field, i))
			e.SetPlaceholderText(placeholder)
			e.SetHexpand(true)
			st.entries[e.GoPointer()] = e
			gtkx.ConnectChanged(e, &s.onSettingsEntry)
			return e
		}
		drop := func(field string, items []string) *gtk.DropDown {
			d := gtk.NewDropDownFromStrings(items)
			d.SetName(fmt.Sprintf("ai-%s:%d", field, i))
			st.drops[d.GoPointer()] = d
			gtkx.ConnectNotify(d, "selected", &s.onSettingsDrop)
			return d
		}
		card.name = entry("name", "a name: work, fast, local …")
		card.provider = drop("provider", providers)
		card.model = entry("model", "the model's ID")
		card.endpoint = entry("endpoint", "the provider's own")
		card.env = entry("env", "none")
		card.env.AddCssClass("mono-entry")
		card.reasoning = drop("reasoning", reasoningNames)
		card.key = entry("key", "Paste a key, then Save")
		card.key.SetVisibility(false)
		card.key.AddCssClass("mono-entry")
		field := func(label string, w *gtk.Widget, col, row, width int) {
			f := gtk.NewBox(gtk.OrientationVerticalValue, 6)
			l := gtk.NewLabel(label)
			l.AddCssClass("label")
			l.SetXalign(0)
			gtkx.Append(f, &l.Widget)
			f.Append(w)
			grid.Attach(&f.Widget, int32(col), int32(row), int32(width), 1)
			f.Unref()
		}
		field("Name", &card.name.Widget, 0, 0, 1)
		field("Provider", &card.provider.Widget, 1, 0, 1)
		field("Model", &card.model.Widget, 0, 1, 1)
		field("Reasoning", &card.reasoning.Widget, 1, 1, 1)
		field("Endpoint", &card.endpoint.Widget, 0, 2, 1)
		field("Key from the variable", &card.env.Widget, 1, 2, 1)
		keyRow := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		keyRow.Append(&card.key.Widget)
		card.save = gtk.NewButtonWithLabel("Save")
		card.forget = gtk.NewButtonWithLabel("Forget")
		for _, b := range []*gtk.Button{card.save, card.forget} {
			b.AddCssClass("outlined")
			keyRow.Append(&b.Widget)
			b.ConnectClicked(&s.onSettingsButton)
		}
		card.save.SetName(fmt.Sprintf("ai-keysave:%d", i))
		card.forget.SetName(fmt.Sprintf("ai-keyforget:%d", i))
		field("API key", &keyRow.Widget, 0, 3, 2)
		keyRow.Unref()
		gtkx.Append(box, &grid.Widget)

		card.fallbackRow = gtk.NewBox(gtk.OrientationHorizontalValue, 16)
		l := gtk.NewLabel("Fallback: answers when the profiles above it fail")
		l.AddCssClass("sub")
		l.SetXalign(0)
		l.SetHexpand(true)
		gtkx.Append(card.fallbackRow, &l.Widget)
		card.fallback = gtk.NewSwitch()
		card.fallback.SetName(fmt.Sprintf("ai-fallback:%d", i))
		card.fallback.SetValign(gtk.AlignCenterValue)
		card.fallback.ConnectStateSet(&s.onSettingsSwitch)
		card.fallbackRow.Append(&card.fallback.Widget)
		box.Append(&card.fallbackRow.Widget)

		gtkx.Append(&a.list, &box.Widget)
		// The card's box owns them now; the card keeps pointers.
		for _, o := range []interface{ Unref() }{&card.title, &card.state, card.name, card.model, card.endpoint, card.env,
			card.key, card.provider, card.reasoning, card.fallback, card.fallbackRow, card.up, card.down, card.remove,
			card.save, card.forget} { // texts and grid went with gtkx.Append
			o.Unref()
		}
		a.cards = append(a.cards, card)
	}
}

// fillAICard sets a card from its profile; saved are the saved keys (err
// when they could not be read).
func (s *shellUI) fillAICard(card *aiCard, i int, p config.AIProfile, saved map[string]uint64, err error) {
	c := s.cfg.Assistant
	card.title.SetText(p.Name + " · " + p.Model)
	setEntry(card.name, p.Name)
	setEntry(card.model, p.Model)
	setEntry(card.endpoint, p.Endpoint)
	setEntry(card.env, p.APIKeyEnv)
	card.provider.SetSelected(uint32(max(0, slices.Index(config.AIProviderIDs(), p.Provider))))
	card.reasoning.SetSelected(uint32(max(0, slices.Index(config.ReasoningEfforts, p.Reasoning))))
	card.fallback.SetActive(slices.Contains(c.Fallback, p.Name))
	card.fallbackRow.SetVisible(p.Name != c.Profile)
	card.up.SetSensitive(i > 0)
	card.down.SetSensitive(i < len(c.Profiles)-1)
	card.remove.SetSensitive(len(c.Profiles) > 1)

	role := ""
	switch {
	case p.Name == c.Profile:
		role = "Answers first. "
	case slices.Contains(c.Fallback, p.Name):
		role = fmt.Sprintf("Fallback %d. ", slices.Index(c.Fallback, p.Name)+1)
	}
	var key string
	switch src := keys.Source(saved, p.Name, p.APIKeyEnv); {
	case err != nil:
		key = "The saved keys cannot be read: " + err.Error()
	case src == "saved":
		key = "Key saved"
	case src != "":
		key = "Key from $" + src
	case strings.HasSuffix(p.Provider, "_compatible"):
		key = "No key (a local server may want none)"
	case p.Name == c.Profile:
		key = "No key yet: the assistant cannot answer"
	default:
		key = "No key yet: this profile is skipped"
	}
	card.state.SetText(role + key)
	toggle(&card.state.Widget, "missing", err != nil || (key[0] == 'N' && !strings.HasSuffix(p.Provider, "_compatible")))
	card.forget.SetVisible(saved[p.Name] != 0)
	if saved[p.Name] != 0 {
		card.key.SetPlaceholderText("Saved; paste another to replace it")
	} else {
		card.key.SetPlaceholderText("Paste a key, then Save")
	}
}

func providerKeyEnv(id string) string {
	for _, p := range config.AIProviders {
		if p.ID == id {
			return p.KeyEnv
		}
	}
	return ""
}

// aiIndex is the profile a control named "ai-FIELD:N" is for.
func aiIndex(name string, c config.Assistant) (string, int, bool) {
	field, n, ok := strings.Cut(strings.TrimPrefix(name, "ai-"), ":")
	i, err := strconv.Atoi(n)
	return field, i, ok && err == nil && i >= 0 && i < len(c.Profiles)
}

// aiEntryChanged applies a profile's text field; false marks it invalid.
func (s *shellUI) aiEntryChanged(name, text string, c *config.Config) bool {
	field, i, ok := aiIndex(name, c.Assistant)
	if !ok {
		return false
	}
	a := &c.Assistant
	a.Profiles = slices.Clone(a.Profiles)
	p := &a.Profiles[i]
	text = strings.TrimSpace(text)
	switch field {
	case "name":
		if text == p.Name {
			return true
		}
		if !config.ValidProfileName(text) {
			return false
		}
		if _, taken := a.ProfileNamed(text); taken {
			return false
		}
		old := p.Name
		p.Name = text
		if a.Profile == old {
			a.Profile = text
		}
		a.Fallback = slices.Clone(a.Fallback)
		for j, f := range a.Fallback {
			if f == old {
				a.Fallback[j] = text
			}
		}
		// The key follows once the name settles (saveSettings).
		st := &s.settings.ai
		moved := false
		for j, r := range st.renames {
			if r[1] == old {
				st.renames[j][1], moved = text, true
			}
		}
		if !moved {
			st.renames = append(st.renames, [2]string{old, text})
		}
		if st.saved[old] != 0 { // shown under its new name meanwhile
			st.saved[text] = st.saved[old]
			delete(st.saved, old)
		}
	case "model":
		if text == "" {
			return false
		}
		p.Model = text
	case "endpoint":
		p.Endpoint = text
	case "env":
		if text != "" && !envNameRe.MatchString(text) {
			return false
		}
		p.APIKeyEnv = text
	case "key":
		return true // saved with its button
	}
	return true
}

// aiDrop applies a profile's drop-down, or the answering profile.
func (s *shellUI) aiDrop(name string, i int, c *config.Config) {
	a := &c.Assistant
	if name == "ai_profile" {
		if i >= 0 && i < len(a.Profiles) {
			a.Profile = a.Profiles[i].Name
			a.Fallback = slices.DeleteFunc(slices.Clone(a.Fallback), func(f string) bool { return f == a.Profile })
		}
		return
	}
	field, n, ok := aiIndex(name, *a)
	if !ok {
		return
	}
	a.Profiles = slices.Clone(a.Profiles)
	p := &a.Profiles[n]
	switch field {
	case "provider":
		if i < 0 || i >= len(config.AIProviders) {
			return
		}
		// The variable follows the provider unless it was set by hand.
		if p.APIKeyEnv == "" || p.APIKeyEnv == providerKeyEnv(p.Provider) {
			p.APIKeyEnv = config.AIProviders[i].KeyEnv
		}
		p.Provider = config.AIProviders[i].ID
	case "reasoning":
		if i >= 0 && i < len(config.ReasoningEfforts) {
			p.Reasoning = config.ReasoningEfforts[i]
		}
	}
}

// aiSwitch applies the AI page's switches; false for a switch not its.
func (s *shellUI) aiSwitch(name string, on bool, c *config.Config) bool {
	a := &c.Assistant
	setTools := func(on bool, names ...string) {
		tools := slices.DeleteFunc(slices.Clone(a.Tools), func(t string) bool { return slices.Contains(names, t) })
		if on {
			tools = append(tools, names...)
		}
		a.Tools = tools
	}
	switch name {
	case "ai_enable":
		a.Enable = on
	case "ai_ask":
		a.AskBeforeChanges = on
	case "ai_web":
		setTools(on, "web_search", "url_fetch")
	case "ai_files":
		setTools(on, "read_file")
	case "ai_bash":
		setTools(on, "bash")
	default:
		field, i, ok := aiIndex(name, *a)
		if !ok || field != "fallback" {
			return false
		}
		in := map[string]bool{}
		for _, f := range a.Fallback {
			in[f] = true
		}
		in[a.Profiles[i].Name] = on
		a.Fallback = nil // in the profiles' order
		for _, p := range a.Profiles {
			if in[p.Name] && p.Name != a.Profile {
				a.Fallback = append(a.Fallback, p.Name)
			}
		}
	}
	return true
}

// aiButton carries out the AI page's buttons; false for a button not its.
func (s *shellUI) aiButton(name string) bool {
	c := s.cfg
	a := &c.Assistant
	if name == "ai_add" {
		p := a.Profiles[0]
		if cur, ok := a.ProfileNamed(a.Profile); ok {
			p = cur
		}
		for n := 2; ; n++ {
			p.Name = fmt.Sprintf("profile-%d", n)
			if _, taken := a.ProfileNamed(p.Name); !taken {
				break
			}
		}
		a.Profiles = append(slices.Clone(a.Profiles), p)
		s.changeSettings(c)
		return true
	}
	if !strings.HasPrefix(name, "ai-") {
		return false
	}
	field, i, ok := aiIndex(name, *a)
	if !ok {
		return true
	}
	profiles := slices.Clone(a.Profiles)
	p := profiles[i]
	switch field {
	case "up", "down":
		j := i - 1
		if field == "down" {
			j = i + 1
		}
		if j < 0 || j >= len(profiles) {
			return true
		}
		profiles[i], profiles[j] = profiles[j], profiles[i]
		a.Profiles = profiles
		var order []string
		for _, q := range profiles {
			if slices.Contains(a.Fallback, q.Name) {
				order = append(order, q.Name)
			}
		}
		a.Fallback = order
	case "remove":
		if len(profiles) == 1 {
			return true
		}
		a.Profiles = slices.Delete(profiles, i, i+1)
		a.Fallback = slices.DeleteFunc(slices.Clone(a.Fallback), func(f string) bool { return f == p.Name })
		if a.Profile == p.Name {
			a.Profile = a.Profiles[0].Name
		}
		s.keysDo("key not removed", func(k *keys.Keys) error { return k.Set(p.Name, "") })
	case "keysave", "keyforget":
		key := ""
		if field == "keysave" {
			key = strings.TrimSpace(s.settings.ai.cards[i].key.GetText())
			if key == "" {
				return true
			}
		}
		s.settings.ai.cards[i].key.SetText("")
		s.setStatus("", false)
		s.keysDo("key not saved", func(k *keys.Keys) error { return k.Set(p.Name, key) })
		return true
	}
	s.changeSettings(c)
	return true
}
