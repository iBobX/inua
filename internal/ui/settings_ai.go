package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/assistant/models"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The AI page: the assistant's switches, which profile answers, and a
// card for each profile (Mister Morph's llm.profiles) with what it is for
// (its abilities) and its fallback switch (the main loop's route). Keys typed here go to internal/assistant/
// keys (the Secret Service, else a file), never to config.toml. Saving one
// may wait for the keyring's prompt, so it runs off the main loop.

// aiSettings are the AI page's controls.
type aiSettings struct {
	enable, ask, web, files, bash gtk.Switch
	profile, decision             gtk.DropDown
	route                         gtk.Label
	list                          gtk.Box
	keysNote                      gtk.Label
	cards                         []*aiCard
	detail                        gtk.Revealer // the panel beside the page: the open profile's settings
	panel                         sidePanel
	scroll                        gtk.ScrolledWindow // the page's
	shown                         int                // the profile the panel showed last, its row kept in place as it closes
	detailHead                    gtk.Label
	detailStack                   gtk.Stack
	open                          int          // the profile the panel shows; -1 for none
	bench                         *benchDialog // the Benchmark dialog, while open
	names, choosers               []string     // the drop-downs' profiles: the one that answers, the one for quick choices
	listen, endpoint, token       gtk.Entry
	serve                         gtk.Switch
	serveNote                     gtk.Label
	tokenShow, tokenCopy, urlCopy gtk.Button
	tokenBusy                     bool

	mcpEnable  gtk.Switch // inua as an MCP server (docs/feat/mcp.md)
	mcpPolicy  gtk.DropDown
	mcpCommand gtk.Entry
	mcpCopy    gtk.Button

	// The assistant's MCP servers: a row each, and the form adding one.

	keys     *keys.Keys
	saved    map[string]uint64 // profiles with a saved key
	savedErr error
	renames  [][2]string // profile renames whose keys move when the settings are saved
}

// aiCard is one profile's row in [PROFILES] and its page of settings in
// the panel beside the page, kept while the profiles keep their count and
// order, so typing in it keeps the caret.
type aiCard struct {
	row                             *gtk.Button // in [PROFILES]; title is its name
	rowMark, rowMeta, rowRole       *gtk.Label  // the provider's initials, provider · model, what it does
	page                            *gtk.Box    // its settings, a page of the panel's stack
	bench                           *gtk.Button
	title, state                    gtk.Label
	name, model, endpoint, env, key *gtk.Entry
	provider, reasoning             *gtk.DropDown
	fallback                        *gtk.Switch
	fallbackRow                     *gtk.Box
	up, down, remove, save, forget  *gtk.Button
	provider2                       string // the provider shown after inua's, one it does not list

	// Abilities as Mister Morph's Console shows them: a chip each, and
	// what the ones on let the profile do.
	chat, decision         *gtk.ToggleButton
	chatMark, decisionMark *gtk.Image
	abilities              *gtk.Label

	// The provider's models: listed in a popover from the button beside
	// the model, searched as you type.
	listModels *gtk.Button
	pop        *gtk.Popover
	popSearch  *gtk.SearchEntry
	popList    *gtk.Box
	popNote    *gtk.Label
	models     []string
}

// reasoningNames are config.ReasoningEfforts as the drop-down shows them.
var reasoningNames = []string{i18n.M("Model default"), i18n.M("Minimal"), i18n.M("Low"), i18n.M("Medium"), i18n.M("High")}

func (s *shellUI) getAISettings(ui *gtkx.Builder) {
	a := &s.settings.ai
	ui.Get("ai_enable", &a.enable)
	ui.Get("ai_ask", &a.ask)
	ui.Get("ai_web", &a.web)
	ui.Get("ai_files", &a.files)
	ui.Get("ai_bash", &a.bash)
	ui.Get("ai_profile", &a.profile)
	ui.Get("ai_decision", &a.decision)
	ui.Get("ai_route", &a.route)
	ui.Get("ai_profiles", &a.list)
	ui.Get("ai_detail", &a.detail)
	ui.Get("scroll_ai", &a.scroll)
	s.initPanel(&a.panel, &a.detail, &a.scroll, 420)
	ui.Get("ai_detail_head", &a.detailHead)
	ui.Get("ai_detail_stack", &a.detailStack)
	var closeDetail gtk.Button
	ui.Get("ai_detail_close", &closeDetail)
	closeDetail.ConnectClicked(&s.onSettingsButton)
	ui.Get("ai_keys_note", &a.keysNote)
	ui.Get("ai_serve_listen", &a.listen)
	ui.Get("ai_serve_enable", &a.serve)
	a.serve.ConnectStateSet(&s.onSettingsSwitch)
	ui.Get("ai_serve_url", &a.endpoint)
	ui.Get("ai_serve_token", &a.token)
	ui.Get("ai_serve_note", &a.serveNote)
	ui.Get("ai_token_show", &a.tokenShow)
	ui.Get("ai_token_copy", &a.tokenCopy)
	ui.Get("ai_url_copy", &a.urlCopy)
	s.settings.entries[a.listen.GoPointer()] = &a.listen
	gtkx.ConnectChanged(&a.listen, &s.onSettingsEntry)
	ui.Get("mcp_enable", &a.mcpEnable)
	ui.Get("mcp_policy", &a.mcpPolicy)
	ui.Get("mcp_command", &a.mcpCommand)
	ui.Get("mcp_copy", &a.mcpCopy)
	a.mcpEnable.ConnectStateSet(&s.onSettingsSwitch)
	s.settings.drops[a.mcpPolicy.GoPointer()] = &a.mcpPolicy
	gtkx.ConnectNotify(&a.mcpPolicy, "selected", &s.onSettingsDrop)
	for _, b := range []*gtk.Button{&a.tokenShow, &a.tokenCopy, &a.urlCopy, &a.mcpCopy} {
		b.ConnectClicked(&s.onSettingsButton)
	}
	var add gtk.Button
	ui.Get("ai_add", &add)
	add.ConnectClicked(&s.onSettingsButton)
	for _, sw := range []*gtk.Switch{&a.enable, &a.ask, &a.web, &a.files, &a.bash} {
		sw.ConnectStateSet(&s.onSettingsSwitch)
	}
	for _, d := range []*gtk.DropDown{&a.profile, &a.decision} {
		s.settings.drops[d.GoPointer()] = d
		gtkx.ConnectNotify(d, "selected", &s.onSettingsDrop)
	}
	a.cards, a.names, a.choosers, a.open, a.shown = nil, nil, nil, -1, -1
	if a.keys == nil {
		a.keys = keys.New()
	}
	a.keysNote.SetText(i18n.Tf("Keys typed here are kept in %s", a.keys.Where()))
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
			s.keysDo(i18n.T("key not moved"), func(k *keys.Keys) error { return k.Rename(r[0], r[1]) })
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
	a.mcpEnable.SetActive(s.cfg.MCP.Enable)
	a.mcpPolicy.SetSelected(uint32(max(0, slices.Index(config.MCPPolicies, s.cfg.MCP.Policy))))
	a.web.SetActive(slices.Contains(c.Tools, "web_search"))
	a.files.SetActive(slices.Contains(c.Tools, "read_file"))
	a.bash.SetActive(slices.Contains(c.Tools, "bash"))
	setEntry(&a.listen, c.ServeListen)
	a.serve.SetActive(c.ServeEnable)
	endpoint := ""
	if host, port, err := net.SplitHostPort(c.ServeListen); err == nil {
		switch host {
		case "", "0.0.0.0":
			host = "127.0.0.1"
		case "::":
			host = "::1"
		}
		endpoint = "http://" + net.JoinHostPort(host, port) + "/runtime"
	}
	setEntry(&a.endpoint, endpoint)
	a.urlCopy.SetSensitive(endpoint != "")
	note := i18n.T("Use this URL and the token in Console. From another computer, use this desktop's reachable address.")
	switch {
	case !c.Enable:
		note = i18n.T("Turn on Assistant to serve this endpoint.")
	case !c.ServeEnable:
		note = i18n.T("HTTP serving is off. The assistant still runs in the background.")
	case a.saved[keys.ServeTokenKey] == 0:
		note = i18n.T("Create a token to allow Console connections.")
	}
	a.serveNote.SetText(note)
	a.tokenShow.SetSensitive(!a.tokenBusy)
	a.tokenCopy.SetSensitive(!a.tokenBusy && a.saved[keys.ServeTokenKey] != 0)
	switch {
	case a.tokenBusy:
		a.tokenShow.SetLabel(i18n.T("Reading keyring…"))
	case a.token.GetVisibility() && a.token.GetText() != "":
		a.tokenShow.SetLabel(i18n.T("Hide token"))
	case a.saved[keys.ServeTokenKey] == 0:
		a.tokenShow.SetLabel(i18n.T("Create token"))
	default:
		a.tokenShow.SetLabel(i18n.T("Show token"))
	}

	names, choosers := aiNames(c)
	if !slices.Equal(names, a.names) {
		list := gtk.NewStringList(names)
		a.profile.SetModel(list)
		list.Unref()
		a.names = names
	}
	if !slices.Equal(choosers, a.choosers) {
		list := gtk.NewStringList(append([]string{i18n.T("The one that answers")}, choosers...))
		a.decision.SetModel(list)
		list.Unref()
		a.choosers = choosers
	}
	a.profile.SetSelected(uint32(max(0, slices.Index(names, c.Profile))))
	a.decision.SetSelected(uint32(slices.Index(choosers, c.Decision) + 1)) // 0: none
	if len(c.Fallback) > 0 {
		a.route.SetText(i18n.Tf("Then, when it fails: %s", strings.Join(c.Fallback, ", ")))
	} else {
		a.route.SetText(i18n.T("The profile asked first; turn on Fallback on others to try them when it fails"))
	}

	if len(a.cards) != len(c.Profiles) {
		s.buildAICards(len(c.Profiles))
	}
	for i, p := range c.Profiles {
		s.fillAICard(a.cards[i], i, p, a.saved, a.savedErr)
	}
	if a.open >= len(a.cards) {
		a.open = -1
	}
	s.showAIDetail()
}

// showAIDetail shows the open profile's settings in the panel, marking its
// row; none closes the panel.
func (s *shellUI) showAIDetail() {
	a := &s.settings.ai
	for i, card := range a.cards {
		toggle(&card.row.Widget, "on", i == a.open)
	}
	if a.open < 0 {
		var row *gtk.Widget
		if a.shown >= 0 && a.shown < len(a.cards) {
			row = &a.cards[a.shown].row.Widget
		}
		s.showPanel(&a.panel, false, row)
		return
	}
	a.shown = a.open
	a.detailStack.SetVisibleChildName(fmt.Sprintf("p%d", a.open))
	name := s.cfg.Assistant.Profiles[a.open].Name
	// A profile's name is letters, digits, "-" and "_": no markup in it.
	a.detailHead.SetMarkup("<span alpha='55%'>[</span>" + strings.ToUpper(name) + "<span alpha='55%'>]</span>")
	s.showPanel(&a.panel, true, &a.cards[a.open].row.Widget)
}

// initials are a provider's for its row: "OpenAI" OA, "Google Gemini" GG.
func initials(name string) string {
	var out []rune
	for _, w := range strings.FieldsFunc(name, func(r rune) bool { return r == ' ' || r == '-' || r == '(' || r == '_' }) {
		if r := []rune(w); len(r) > 0 && len(out) < 2 {
			out = append(out, unicode.ToUpper(r[0]))
		}
	}
	if len(out) == 1 {
		if r := []rune(name); len(r) > 1 {
			out = append(out, unicode.ToLower(r[1]))
		}
	}
	return string(out)
}

// aiNames are the profiles that answer, for the drop-down of the one
// that does, and those that make quick choices.
func aiNames(c config.Assistant) (answer, choose []string) {
	for _, p := range c.Profiles {
		if p.Can(config.AbilityText) {
			answer = append(answer, p.Name)
		}
		if p.Can(config.AbilityDecision) {
			choose = append(choose, p.Name)
		}
	}
	return answer, choose
}

// buildAICards makes n empty cards; fillAICard sets them.
func (s *shellUI) buildAICards(n int) {
	st := &s.settings
	a := &st.ai
	for _, card := range a.cards { // their widgets go with the rows and pages
		for _, e := range []*gtk.Entry{card.name, card.model, card.endpoint, card.key} {
			delete(st.entries, e.GoPointer())
		}
		delete(st.drops, card.provider.GoPointer())
		delete(st.drops, card.reasoning.GoPointer())
		card.pop.Unparent()
		card.pop.Unref()
		a.detailStack.Remove(&card.page.Widget)
	}
	gtkx.Clear(&a.list)
	a.cards = nil
	providers := aiProviderNames()
	if s.onAIAbility == nil {
		s.onAIAbility = func(t gtk.ToggleButton) {
			if s.settings.filling {
				return
			}
			c := s.cfg
			s.aiSwitch(t.GetName(), t.GetActive(), &c)
			s.changeSettings(c)
		}
		s.onAIModelSearch = func(e gtk.SearchEntry) {
			for _, card := range s.settings.ai.cards {
				if card.popSearch.GoPointer() == e.GoPointer() {
					s.showAIModels(card)
				}
			}
		}
	}
	for i := range n {
		card := &aiCard{}

		// Its row in [PROFILES]: the provider's initials, the name, the
		// provider and model, and what it does.
		card.row = gtk.NewButton()
		card.row.AddCssClass("ai-row")
		card.row.SetName(fmt.Sprintf("ai-open:%d", i))
		card.row.ConnectClicked(&s.onSettingsButton)
		line := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		card.rowMark = gtk.NewLabel("")
		card.rowMark.AddCssClass("ai-mark")
		card.rowMark.SetValign(gtk.AlignCenterValue)
		line.Append(&card.rowMark.Widget)
		texts := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		texts.SetHexpand(true)
		card.title = *gtk.NewLabel("")
		card.title.AddCssClass("title")
		card.title.SetXalign(0)
		card.title.SetEllipsize(3) // end
		card.rowMeta = gtk.NewLabel("")
		card.rowMeta.AddCssClass("sub")
		card.rowMeta.SetXalign(0)
		card.rowMeta.SetEllipsize(3)
		texts.Append(&card.title.Widget)
		texts.Append(&card.rowMeta.Widget)
		gtkx.Append(line, &texts.Widget)
		card.rowRole = gtk.NewLabel("")
		card.rowRole.AddCssClass("sub")
		card.rowRole.SetValign(gtk.AlignCenterValue)
		line.Append(&card.rowRole.Widget)
		caret := gtk.NewImageFromIconName("go-next-symbolic")
		caret.SetValign(gtk.AlignCenterValue)
		gtkx.Append(line, &caret.Widget)
		card.row.SetChild(&line.Widget)
		line.Unref()
		a.list.Append(&card.row.Widget)

		// Its settings, a page of the panel.
		card.page = gtk.NewBox(gtk.OrientationVerticalValue, 14)
		card.page.AddCssClass("ai-card")
		card.state = *gtk.NewLabel("")
		card.state.AddCssClass("sub")
		card.state.SetXalign(0)
		card.state.SetWrap(true)
		card.page.Append(&card.state.Widget)

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
		card.name = entry("name", i18n.T("a name: work, fast, local …"))
		card.provider = drop("provider", providers)
		card.model = entry("model", i18n.T("the model's ID"))
		card.endpoint = entry("endpoint", i18n.T("the provider's own"))
		translatedReasoning := make([]string, len(reasoningNames))
		for j, name := range reasoningNames {
			translatedReasoning[j] = i18n.T(name)
		}
		card.reasoning = drop("reasoning", translatedReasoning)
		card.key = entry("key", i18n.T("Paste a key, then Save"))
		card.key.SetVisibility(false)
		card.key.AddCssClass("mono-entry")
		field := func(label string, w *gtk.Widget) {
			f := gtk.NewBox(gtk.OrientationVerticalValue, 6)
			l := gtk.NewLabel(label)
			l.AddCssClass("label")
			l.SetXalign(0)
			gtkx.Append(f, &l.Widget)
			f.Append(w)
			gtkx.Append(card.page, &f.Widget)
		}
		field(i18n.T("Name"), &card.name.Widget)
		field(i18n.T("Provider"), &card.provider.Widget)
		modelRow := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		modelRow.Append(&card.model.Widget)
		card.listModels = gtk.NewButtonFromIconName("system-search-symbolic")
		card.listModels.AddCssClass("outlined")
		card.listModels.SetName(fmt.Sprintf("ai-models:%d", i))
		card.listModels.SetTooltipText(i18n.T("List the provider's models"))
		card.listModels.ConnectClicked(&s.onSettingsButton)
		modelRow.Append(&card.listModels.Widget)
		s.buildAIModelsPop(card)
		field(i18n.T("Model"), &modelRow.Widget)
		modelRow.Unref()
		field(i18n.T("Reasoning"), &card.reasoning.Widget)
		field(i18n.T("Endpoint"), &card.endpoint.Widget)
		abilities := gtk.NewBox(gtk.OrientationVerticalValue, 6)
		chips := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		chip := func(ability, label string) (*gtk.ToggleButton, *gtk.Image) {
			t := gtk.NewToggleButton()
			t.AddCssClass("ability")
			t.SetName(fmt.Sprintf("ai-%s:%d", ability, i))
			inner := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
			mark := gtk.NewImageFromIconName("object-select-symbolic")
			inner.Append(&mark.Widget)
			l := gtk.NewLabel(label)
			gtkx.Append(inner, &l.Widget)
			t.SetChild(&inner.Widget)
			inner.Unref()
			t.ConnectToggled(&s.onAIAbility)
			chips.Append(&t.Widget)
			return t, mark
		}
		card.chat, card.chatMark = chip(config.AbilityText, i18n.T("Chat"))
		card.decision, card.decisionMark = chip(config.AbilityDecision, i18n.T("Decision"))
		gtkx.Append(abilities, &chips.Widget)
		card.abilities = gtk.NewLabel("")
		card.abilities.AddCssClass("sub")
		card.abilities.SetXalign(0)
		card.abilities.SetWrap(true)
		abilities.Append(&card.abilities.Widget)
		field(i18n.T("Abilities"), &abilities.Widget)
		keyRow := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		keyRow.Append(&card.key.Widget)
		card.save = gtk.NewButtonWithLabel(i18n.T("Save"))
		card.forget = gtk.NewButtonWithLabel(i18n.T("Forget"))
		for _, b := range []*gtk.Button{card.save, card.forget} {
			b.AddCssClass("outlined")
			keyRow.Append(&b.Widget)
			b.ConnectClicked(&s.onSettingsButton)
		}
		card.save.SetName(fmt.Sprintf("ai-keysave:%d", i))
		card.forget.SetName(fmt.Sprintf("ai-keyforget:%d", i))
		field(i18n.T("API key"), &keyRow.Widget)

		card.fallbackRow = gtk.NewBox(gtk.OrientationHorizontalValue, 16)
		l := gtk.NewLabel(i18n.T("Fallback: answers when the profiles above it fail"))
		l.AddCssClass("sub")
		l.SetXalign(0)
		l.SetHexpand(true)
		l.SetWrap(true)
		gtkx.Append(card.fallbackRow, &l.Widget)
		card.fallback = gtk.NewSwitch()
		card.fallback.SetName(fmt.Sprintf("ai-fallback:%d", i))
		card.fallback.SetValign(gtk.AlignCenterValue)
		card.fallback.ConnectStateSet(&s.onSettingsSwitch)
		card.fallbackRow.Append(&card.fallback.Widget)
		card.page.Append(&card.fallbackRow.Widget)

		// Benchmark, the order (fallbacks are tried in it), and Remove.
		actions := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		actions.AddCssClass("ai-actions")
		button := func(action, icon, label, tip string) *gtk.Button {
			b := gtk.NewButtonFromIconName(icon)
			if label != "" {
				b = gtk.NewButton()
				inner := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
				img := gtk.NewImageFromIconName(icon)
				gtkx.Append(inner, &img.Widget)
				lb := gtk.NewLabel(label)
				gtkx.Append(inner, &lb.Widget)
				b.SetChild(&inner.Widget)
				inner.Unref()
			}
			b.AddCssClass("outlined")
			b.SetName(fmt.Sprintf("ai-%s:%d", action, i))
			b.SetTooltipText(tip)
			b.ConnectClicked(&s.onSettingsButton)
			actions.Append(&b.Widget)
			return b // unreferenced with the card's other widgets
		}
		card.bench = button("bench", "inua-benchmark-symbolic", i18n.T("Benchmark"), i18n.T("Try the profile as inua uses it"))
		gap := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		gap.SetHexpand(true)
		gtkx.Append(actions, &gap.Widget)
		card.up = button("up", "pan-up-symbolic", "", i18n.T("Move up (fallbacks are tried in this order)"))
		card.down = button("down", "pan-down-symbolic", "", i18n.T("Move down"))
		card.remove = button("remove", "user-trash-symbolic", i18n.T("Remove"), i18n.T("Remove this profile"))
		card.remove.AddCssClass("danger")
		gtkx.Append(card.page, &actions.Widget)

		a.detailStack.AddNamed(&card.page.Widget, fmt.Sprintf("p%d", i))
		// The row and the page own them now; the card keeps pointers.
		for _, o := range []interface{ Unref() }{card.row, card.rowMark, &card.title, card.rowMeta, card.rowRole, card.page,
			&card.state, card.name, card.model, card.endpoint, card.key, card.provider, card.reasoning, card.fallback,
			card.fallbackRow, card.up, card.down, card.remove, card.bench, card.save, card.forget, card.listModels,
			card.chat, card.decision, card.chatMark, card.decisionMark, card.abilities, keyRow, abilities} {
			o.Unref()
		}
		a.cards = append(a.cards, card)
	}
}

// aiProviderNames are config.AIProviders as the drop-down shows them.
func aiProviderNames() []string {
	names := make([]string, len(config.AIProviders))
	for i, p := range config.AIProviders {
		names[i] = p.Name
	}
	return names
}

// fillAICard sets a card from its profile; saved are the saved keys (err
// when they could not be read).
func (s *shellUI) fillAICard(card *aiCard, i int, p config.AIProfile, saved map[string]uint64, err error) {
	c := s.cfg.Assistant
	card.title.SetText(p.Name)
	provider := p.Provider
	if i := slices.Index(config.AIProviderIDs(), p.Provider); i >= 0 {
		provider = config.AIProviders[i].Name
	}
	card.rowMeta.SetText(provider + " · " + p.Model)
	card.rowMark.SetText(initials(provider))
	setEntry(card.name, p.Name)
	setEntry(card.model, p.Model)
	setEntry(card.endpoint, p.Endpoint)
	// A provider inua does not list shows after its own, kept as it is.
	known := slices.Index(config.AIProviderIDs(), p.Provider)
	extra := ""
	if known < 0 {
		extra, known = p.Provider, len(config.AIProviders)
	}
	if extra != card.provider2 {
		names := aiProviderNames()
		if extra != "" {
			names = append(names, extra)
		}
		list := gtk.NewStringList(names)
		card.provider.SetModel(list)
		list.Unref()
		card.provider2 = extra
	}
	card.provider.SetSelected(uint32(known))
	card.reasoning.SetSelected(uint32(max(0, slices.Index(config.ReasoningEfforts, p.Reasoning))))
	text, choose := p.Can(config.AbilityText), p.Can(config.AbilityDecision)
	card.chat.SetActive(text)
	card.decision.SetActive(choose)
	card.chatMark.SetVisible(text)
	card.decisionMark.SetVisible(choose)
	// The one that answers keeps chatting; a profile keeps one of the two;
	// TypeSafe only makes quick choices.
	only := config.DecisionOnly(p.Provider)
	card.chat.SetSensitive(!only && p.Name != c.Profile && (!text || choose))
	card.decision.SetSensitive(!only && (!choose || text))
	switch {
	case only:
		card.chat.SetTooltipText(i18n.T("This provider makes quick choices only"))
	case p.Name == c.Profile:
		card.chat.SetTooltipText(i18n.T("The profile that answers chats"))
	default:
		card.chat.SetTooltipText("")
	}
	card.listModels.SetSensitive(!only)
	if only {
		card.listModels.SetTooltipText(i18n.T("This provider does not list its models"))
	} else {
		card.listModels.SetTooltipText(i18n.T("List the provider's models"))
	}
	card.abilities.SetText(abilitySummary(text, choose))
	card.fallback.SetActive(slices.Contains(c.Fallback, p.Name))
	card.fallbackRow.SetVisible(p.Name != c.Profile && text)
	card.up.SetSensitive(i > 0)
	card.down.SetSensitive(i < len(c.Profiles)-1)
	card.remove.SetSensitive(len(c.Profiles) > 1)

	role := ""
	switch {
	case p.Name == c.Profile:
		role = i18n.T("Answers first. ")
	case slices.Contains(c.Fallback, p.Name):
		role = i18n.Tf("Fallback %d. ", slices.Index(c.Fallback, p.Name)+1)
	}
	if p.Name == c.DecisionProfile() {
		role += i18n.T("Makes the quick choices. ")
	}
	var key string
	switch {
	case err != nil:
		key = i18n.Tf("The saved keys cannot be read: %s", err.Error())
	case saved[p.Name] != 0:
		key = i18n.T("Key saved")
	case strings.HasSuffix(p.Provider, "_compatible"):
		key = i18n.T("No key (a local server may want none)")
	case p.Name == c.Profile:
		key = i18n.T("No key yet: the assistant cannot answer")
	default:
		key = i18n.T("No key yet: this profile is skipped")
	}
	card.state.SetText(role + key)
	missing := err != nil || (saved[p.Name] == 0 && !strings.HasSuffix(p.Provider, "_compatible"))
	toggle(&card.state.Widget, "missing", missing)
	var roles []string
	switch {
	case p.Name == c.Profile:
		roles = append(roles, i18n.T("Answers"))
	case slices.Contains(c.Fallback, p.Name):
		roles = append(roles, i18n.Tf("Fallback %d", slices.Index(c.Fallback, p.Name)+1))
	}
	if p.Name == c.DecisionProfile() {
		roles = append(roles, i18n.T("Quick choices"))
	}
	if missing {
		roles = append(roles, i18n.T("No key"))
	}
	card.rowRole.SetText(strings.Join(roles, " · "))
	toggle(&card.rowRole.Widget, "missing", missing)
	card.forget.SetVisible(saved[p.Name] != 0)
	if saved[p.Name] != 0 {
		card.key.SetPlaceholderText(i18n.T("Saved; paste another to replace it"))
	} else {
		card.key.SetPlaceholderText(i18n.T("Paste a key, then Save"))
	}
}

// aiIndex is the profile a control named "ai-FIELD:N" is for.
func aiIndex(name string, c config.Assistant) (string, int, bool) {
	field, n, ok := strings.Cut(strings.TrimPrefix(name, "ai-"), ":")
	i, err := strconv.Atoi(n)
	return field, i, ok && err == nil && i >= 0 && i < len(c.Profiles)
}

// aiEntryChanged applies a profile's text field; false marks it invalid.
func (s *shellUI) aiEntryChanged(name, text string, c *config.Config) bool {
	if name == "ai-serve-listen" {
		text = strings.TrimSpace(text)
		_, port, err := net.SplitHostPort(text)
		n, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || n < 1 || n > 65535 {
			return false
		}
		c.Assistant.ServeListen = text
		return true
	}
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
		if a.Decision == old {
			a.Decision = text
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
	case "key":
		return true // saved with its button
	}
	return true
}

// aiDrop applies a profile's drop-down, or the answering profile.
func (s *shellUI) aiDrop(name string, i int, c *config.Config) {
	a := &c.Assistant
	answer, choose := aiNames(*a)
	if name == "ai_profile" {
		if i >= 0 && i < len(answer) {
			a.Profile = answer[i]
			a.Fallback = slices.DeleteFunc(slices.Clone(a.Fallback), func(f string) bool { return f == a.Profile })
		}
		return
	}
	if name == "ai_decision" {
		switch {
		case i == 0:
			a.Decision = ""
		case i > 0 && i <= len(choose):
			a.Decision = choose[i-1]
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
			return // or the one inua does not list, as it is
		}
		id := config.AIProviders[i].ID
		if config.DecisionOnly(id) && p.Name == a.Profile {
			return // the one that answers chats
		}
		p.Provider = id
		if config.DecisionOnly(id) {
			p.Abilities = []string{config.AbilityDecision}
			a.Fallback = slices.DeleteFunc(slices.Clone(a.Fallback), func(f string) bool { return f == p.Name })
		}
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
	case "ai_serve_enable":
		a.ServeEnable = on
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
		if !ok {
			return false
		}
		switch field {
		case "text", "decision":
			a.Profiles = slices.Clone(a.Profiles)
			p := &a.Profiles[i]
			if field == "text" && !on && p.Name == a.Profile {
				return true // the one that answers answers
			}
			p.Abilities = config.WithAbility(p.Abilities, field, on)
			if !p.Can(config.AbilityText) {
				a.Fallback = slices.DeleteFunc(slices.Clone(a.Fallback), func(f string) bool { return f == p.Name })
			}
			if !p.Can(config.AbilityDecision) && a.Decision == p.Name {
				a.Decision = ""
			}
			return true
		case "fallback":
		default:
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
	if name == "ai_detail_close" {
		s.settings.ai.open = -1
		s.showAIDetail()
		return true
	}
	if name == "mcp_copy" {
		clip := s.settings.win.GetClipboard()
		clip.SetText(s.settings.ai.mcpCommand.GetText())
		clip.Unref()
		s.setStatus(i18n.T("Command copied"), false)
		return true
	}
	if name == "ai_url_copy" {
		clip := s.settings.win.GetClipboard()
		clip.SetText(s.settings.ai.endpoint.GetText())
		clip.Unref()
		s.setStatus(i18n.T("Endpoint URL copied"), false)
		return true
	}
	if name == "ai_token_show" || name == "ai_token_copy" {
		a := &s.settings.ai
		if a.tokenBusy {
			return true
		}
		if name == "ai_token_show" && a.token.GetVisibility() && a.token.GetText() != "" {
			a.token.SetText("")
			a.token.SetVisibility(false)
			s.showSettings()
			return true
		}
		a.tokenBusy = true
		s.showSettings()
		win, k := s.settings.win, a.keys
		go func() {
			token, err := k.ServeToken(name == "ai_token_show")
			saved, savedErr := k.Saved()
			gtkx.Post(func() {
				a.tokenBusy = false
				a.saved, a.savedErr = saved, savedErr
				if s.settings.win == nil {
					return
				}
				if s.settings.win == win {
					switch {
					case err != nil:
						s.setStatus(i18n.Tf("Cannot read the Console token: %s", err.Error()), true)
					case token == "":
						s.setStatus(i18n.T("No Console token. Create one first."), true)
					case name == "ai_token_copy":
						clip := win.GetClipboard()
						clip.SetText(token)
						clip.Unref()
						s.setStatus(i18n.T("Token copied"), false)
					default:
						a.token.SetText(token)
						a.token.SetVisibility(true)
						s.setStatus("", false)
					}
				}
				s.showSettings()
			})
		}()
		return true
	}
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
		s.settings.ai.open = len(a.Profiles) - 1 // its settings, to fill in
		s.changeSettings(c)
		return true
	}
	if !strings.HasPrefix(name, "ai-") {
		return false
	}
	field, i, ok := aiIndex(name, *a)
	if !ok || i >= len(s.settings.ai.cards) {
		return true
	}
	switch card := s.settings.ai.cards[i]; field {
	case "open":
		st := &s.settings.ai
		if st.open == i {
			st.open = -1 // a second click closes it
		} else {
			st.open = i
		}
		s.showAIDetail()
		return true
	case "bench":
		s.benchAI(a.Profiles[i].Name)
		return true
	case "models":
		s.listAIModels(card, a.Profiles[i])
		return true
	case "modelpick":
		return true // the popover's buttons, handled by pickAIModel
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
		switch st := &s.settings.ai; st.open { // the panel follows the profile
		case i:
			st.open = j
		case j:
			st.open = i
		}
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
		switch st := &s.settings.ai; {
		case st.open == i:
			st.open = -1
		case st.open > i:
			st.open--
		}
		a.Fallback = slices.DeleteFunc(slices.Clone(a.Fallback), func(f string) bool { return f == p.Name })
		if a.Profile == p.Name {
			a.Profile = a.Profiles[0].Name
		}
		if a.Decision == p.Name {
			a.Decision = ""
		}
		s.keysDo(i18n.T("key not removed"), func(k *keys.Keys) error { return k.Set(p.Name, "") })
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
		s.keysDo(i18n.T("key not saved"), func(k *keys.Keys) error { return k.Set(p.Name, key) })
		return true
	}
	s.changeSettings(c)
	return true
}

// abilitySummary says what a profile with these abilities does.
func abilitySummary(chat, decision bool) string {
	switch {
	case chat && decision:
		return i18n.T("Chats and runs tasks with inua's tools. Also makes quick choices from a list, as the games' AI players do.")
	case chat:
		return i18n.T("Chats and runs tasks with inua's tools. The games' AI players do not use it.")
	case decision:
		return i18n.T("Makes only quick choices from a list (yes or no, one of a few), as the games' AI players do. It cannot chat or run tasks.")
	}
	return i18n.T("Neither chats nor makes quick choices: inua does not use it.")
}

// buildAIModelsPop makes the popover of card's model button.
func (s *shellUI) buildAIModelsPop(card *aiCard) {
	card.pop = gtk.NewPopover()
	card.pop.AddCssClass("ai-models")
	card.pop.SetParent(&card.listModels.Widget)
	box := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	card.popSearch = gtk.NewSearchEntry()
	card.popSearch.SetPlaceholderText(i18n.T("Search models"))
	card.popSearch.ConnectSearchChanged(&s.onAIModelSearch)
	box.Append(&card.popSearch.Widget)
	card.popNote = gtk.NewLabel("")
	card.popNote.AddCssClass("sub")
	card.popNote.SetXalign(0)
	card.popNote.SetWrap(true)
	card.popNote.SetMaxWidthChars(40)
	box.Append(&card.popNote.Widget)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetMinContentWidth(320)
	scroll.SetMaxContentHeight(360)
	scroll.SetPropagateNaturalHeight(true)
	card.popList = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	scroll.SetChild(&card.popList.Widget)
	gtkx.Append(box, &scroll.Widget)
	card.pop.SetChild(&box.Widget)
	box.Unref()
	for _, o := range []interface{ Unref() }{card.popSearch, card.popNote, card.popList} {
		o.Unref()
	}
}

// listAIModels opens card's list of models, asking the provider with the
// key typed, else the one saved (the keyring may ask), else none.
func (s *shellUI) listAIModels(card *aiCard, p config.AIProfile) {
	a := &s.settings.ai
	card.models = nil
	card.popSearch.SetText("")
	card.popNote.SetText(i18n.T("Listing models…"))
	card.popNote.SetVisible(true)
	gtkx.Clear(card.popList)
	card.pop.Popup()
	typed, saved, k := strings.TrimSpace(card.key.GetText()), a.saved[p.Name] != 0, a.keys
	go func() {
		key, err := typed, error(nil)
		if key == "" && saved {
			key, err = k.Get(p.Name)
		}
		var list []string
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			list, err = models.List(ctx, p.Provider, p.Endpoint, key)
			cancel()
		}
		gtkx.Post(func() {
			if !slices.Contains(s.settings.ai.cards, card) {
				return // the page was rebuilt
			}
			switch {
			case errors.Is(err, models.ErrNoList):
				card.popNote.SetText(i18n.T("This provider does not list its models"))
			case errors.Is(err, models.ErrNoEndpoint):
				card.popNote.SetText(i18n.T("Type the server's endpoint first."))
			case err != nil:
				card.popNote.SetText(i18n.Tf("Cannot list the models: %s", err.Error()))
			case len(list) == 0:
				card.popNote.SetText(i18n.T("The provider lists no models."))
			default:
				card.models = list
				s.showAIModels(card)
			}
		})
	}()
}

// showAIModels lists card's models that match its search, as buttons.
func (s *shellUI) showAIModels(card *aiCard) {
	if card.models == nil {
		return
	}
	gtkx.Clear(card.popList)
	q := strings.ToLower(strings.TrimSpace(card.popSearch.GetText()))
	n := 0
	for _, id := range card.models {
		if q != "" && !strings.Contains(strings.ToLower(id), q) {
			continue
		}
		b := gtk.NewButtonWithLabel(id)
		b.AddCssClass("flat")
		b.AddCssClass("ai-model")
		if l := b.GetChild(); l != nil {
			gtk.LabelNewFromInternalPtr(l.GoPointer()).SetXalign(0)
		}
		if s.onAIModelPick == nil {
			s.onAIModelPick = func(b gtk.Button) { s.pickAIModel(b) }
		}
		b.ConnectClicked(&s.onAIModelPick)
		gtkx.Append(card.popList, &b.Widget)
		n++
	}
	card.popNote.SetVisible(n == 0)
	if n == 0 {
		card.popNote.SetText(i18n.T("No model matches."))
	}
}

// pickAIModel puts the model clicked in its card's model field.
func (s *shellUI) pickAIModel(b gtk.Button) {
	for _, card := range s.settings.ai.cards {
		if b.IsAncestor(&card.pop.Widget) {
			card.model.SetText(b.GetLabel())
			card.pop.Popdown()
			return
		}
	}
}
