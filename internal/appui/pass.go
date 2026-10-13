package appui

import (
	"log"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/pass"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// passClearAfter is how long a copied value stays on the clipboard.
const passClearAfter = 45 * time.Second

// masked stands for a hidden value.
const passMasked = "••••••••••"

// The forms of the detail column.
const (
	formMoveCategory = "move-category"
	formMoveEntry    = "move-entry"
	formNew          = "new"
	formDelete       = "delete"
)

type passApp struct {
	c        *Ctx
	prefs    pass.Prefs
	stores   []string
	store    pass.Store
	keyring  string // inua-keyring's folder, not renamed or moved
	entries  []string
	category string // the chosen category; "" for the top level
	picked   bool   // a category is chosen
	selected string // the open entry's name; "" for none

	// The open entry, decrypted; gen drops a decryption that comes back
	// after another entry was opened.
	raw     string
	entry   pass.Entry
	otp     *pass.OTP
	gen     int
	reveal  map[string]bool   // the secret rows shown, by row
	values  map[string]string // what each row's copy button copies
	otpCode *gtk.Label
	otpLeft *gtk.Label

	form       string // the form shown, one of form…
	formFrom   string // what it renames, moves or deletes
	editNew    bool   // the editor writes a new entry, named by the form
	newName    string
	busy       bool // a pass command runs
	clipGen    int
	filling    bool
	categories []string // the form's menu of categories

	categoryList, entryList, fields     gtk.Box
	storeMenu                           gtk.DropDown
	storePath, categoryTitle, emptyText gtk.Label
	entryTitle, entryPath, entryState   gtk.Label
	editTitle, editError                gtk.Label
	formTitle, formText, formResult     gtk.Label
	formCategoryBox, formSubBox         gtk.Box
	formCategory, formSub               gtk.Entry
	formCategories                      gtk.DropDown
	formOK, moveCategory, newEntry      gtk.Button
	moveEntry, editEntry, deleteEntry   gtk.Button
	filter                              gtk.SearchEntry
	detail                              gtk.Stack
	editor                              gtk.TextView
	win                                 *gtk.ApplicationWindow
	onButton                            func(gtk.Button)
	onFilter                            func(gtk.SearchEntry)
	onStore, onFormPick                 func(uintptr, uintptr, uintptr)
	onFormChanged                       func(uintptr, uintptr)
	onFormActivate                      func(gtk.Entry)
	onTick                              glib.SourceFunc
	onClear                             glib.SourceFunc
	dialog                              *gtk.FileDialog
	onChosen                            gio.AsyncReadyCallback
}

func (a *passApp) build(c *Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.win = &gtk.ApplicationWindow{}
	a.reveal, a.values = map[string]bool{}, map[string]string{}
	a.keyring = c.config().Keyring.Folder
	var addStore, editCancel, editSave, formCancel gtk.Button
	ui := c.Load("pass.ui")
	ui.Get("pass", a.win)
	for id, w := range map[string]gobject.Ptr{
		"categories": &a.categoryList, "entries": &a.entryList, "fields": &a.fields,
		"store_menu": &a.storeMenu, "store_path": &a.storePath, "category_title": &a.categoryTitle,
		"empty_text": &a.emptyText, "entry_title": &a.entryTitle, "entry_path": &a.entryPath,
		"entry_state": &a.entryState, "edit_title": &a.editTitle, "edit_error": &a.editError,
		"form_title": &a.formTitle, "form_text": &a.formText, "form_result": &a.formResult,
		"form_category_box": &a.formCategoryBox, "form_sub_box": &a.formSubBox,
		"form_category": &a.formCategory, "form_sub": &a.formSub, "form_categories": &a.formCategories,
		"form_ok": &a.formOK, "move_category": &a.moveCategory, "new_entry": &a.newEntry,
		"move_entry": &a.moveEntry, "edit_entry": &a.editEntry, "delete_entry": &a.deleteEntry,
		"filter": &a.filter, "detail": &a.detail, "editor": &a.editor,
		"add_store": &addStore, "edit_cancel": &editCancel, "edit_save": &editSave, "form_cancel": &formCancel,
	} {
		ui.Get(id, w)
	}
	ui.Close()

	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	a.onFilter = func(gtk.SearchEntry) { a.showEntries() }
	a.onStore = func(uintptr, uintptr, uintptr) {
		if i := int(a.storeMenu.GetSelected()); !a.filling && i < len(a.stores) && a.stores[i] != a.store.Dir {
			a.useStore(a.stores[i])
		}
	}
	a.onFormPick = func(uintptr, uintptr, uintptr) {
		if i := int(a.formCategories.GetSelected()); !a.filling && i > 0 && i <= len(a.categories) {
			a.formCategory.SetText(a.categories[i-1])
		}
	}
	a.onFormChanged = func(uintptr, uintptr) { a.checkForm() }
	a.onFormActivate = func(gtk.Entry) { a.button("form-ok") }
	a.onChosen = func(_, result, _ uintptr) {
		d := a.dialog
		a.dialog = nil
		if d == nil {
			return
		}
		defer d.Unref()
		f, err := d.SelectFolderFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || f == nil {
			return // cancelled
		}
		dir := f.GetPath()
		unrefObject(f)
		a.addStore(dir)
	}
	a.onTick = func(uintptr) bool {
		a.tickOTP()
		return true
	}
	for _, b := range []*gtk.Button{&addStore, &editCancel, &editSave, &formCancel, &a.formOK,
		&a.moveCategory, &a.newEntry, &a.moveEntry, &a.editEntry, &a.deleteEntry} {
		b.ConnectClicked(&a.onButton)
	}
	a.filter.ConnectSearchChanged(&a.onFilter)
	gtkx.ConnectNotify(&a.storeMenu, "selected", &a.onStore)
	gtkx.ConnectNotify(&a.formCategories, "selected", &a.onFormPick)
	for _, e := range []*gtk.Entry{&a.formCategory, &a.formSub} {
		gtkx.ConnectChanged(e, &a.onFormChanged)
		e.ConnectActivate(&a.onFormActivate)
	}

	if err := store.Load(pass.StoreName, &a.prefs); err != nil {
		log.Printf("pass: %v", err)
	}
	a.stores = pass.Stores(a.prefs)
	a.showStores()
	a.useStore(pass.Current(a.prefs, a.stores))
	glib.TimeoutAdd(1000, &a.onTick, 0)
	return a.win
}

func (a *passApp) themeChanged() {}

// --- stores ---

// showStores fills the store menu, shown when there is a choice.
func (a *passApp) showStores() {
	a.filling = true
	defer func() { a.filling = false }()
	a.storeMenu.SetModel(gtk.NewStringList(pass.ShortNames(a.stores)))
	if i := slices.Index(a.stores, a.store.Dir); i >= 0 {
		a.storeMenu.SetSelected(uint32(i))
	}
	a.storeMenu.SetVisible(len(a.stores) > 1)
}

// useStore opens a store and lists it.
func (a *passApp) useStore(dir string) {
	a.store = pass.Store{Dir: dir}
	a.storePath.SetText(pass.Tilde(dir))
	a.storePath.SetTooltipText(dir)
	if dir != "" && dir != a.prefs.Store {
		a.prefs.Store = dir
		a.save()
	}
	a.picked, a.category = false, ""
	a.closeEntry()
	a.reload("")
	if i := slices.Index(a.stores, dir); i >= 0 {
		a.filling = true
		a.storeMenu.SetSelected(uint32(i))
		a.filling = false
	}
}

func (a *passApp) save() {
	if err := store.Save(pass.StoreName, a.prefs); err != nil {
		log.Printf("pass: %v", err)
	}
}

// chooseStore asks for another store's folder.
func (a *passApp) chooseStore() {
	if a.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("A password store: a folder with a .gpg-id"))
	a.dialog = d
	d.SelectFolder(&a.win.Window, nil, &a.onChosen, 0)
}

// addStore adds a store chosen and opens it.
func (a *passApp) addStore(dir string) {
	if !pass.IsStore(dir) {
		a.showEmpty(i18n.Tf("%s is not a password store: it has no .gpg-id (pass init makes one).", dir))
		return
	}
	if !slices.Contains(a.prefs.Added, dir) {
		a.prefs.Added = append(a.prefs.Added, dir)
	}
	a.stores = pass.Stores(a.prefs)
	a.useStore(dir)
	a.showStores()
}

// --- lists ---

// reload reads the store's entries again and shows them; open, when
// given, is opened.
func (a *passApp) reload(open string) {
	if a.store.Dir == "" {
		a.entries = nil
		a.showCategories()
		a.showEntries()
		a.showEmpty(i18n.T("No password store. pass init YOUR-GPG-KEY-ID makes one in ~/.password-store; another folder can be added below."))
		return
	}
	entries, err := a.store.Entries()
	if err != nil {
		log.Printf("pass: %v", err)
	}
	a.entries = entries
	cats := pass.Categories(entries)
	if open != "" {
		a.category, _ = pass.Split(open)
		a.picked = true
	}
	if !a.picked || !slices.ContainsFunc(cats, func(c pass.Category) bool { return c.Name == a.category }) {
		a.picked, a.category = len(cats) > 0, ""
		if len(cats) > 0 {
			a.category = cats[0].Name
		}
	}
	a.showCategories()
	a.showEntries()
	switch {
	case open != "":
		a.openEntry(open)
	case a.selected != "" && slices.Contains(entries, a.selected):
	case len(entries) == 0:
		a.closeEntry()
		a.showEmpty(i18n.T("The store is empty. + makes an entry."))
	default:
		a.closeEntry()
		a.showEmpty(i18n.T("Choose an entry."))
	}
}

func (a *passApp) categoryName(c string) string {
	if c == "" {
		return i18n.T("Top level")
	}
	return c
}

func (a *passApp) showCategories() {
	gtkx.Clear(&a.categoryList)
	for _, c := range pass.Categories(a.entries) {
		row := text(a.categoryName(c.Name), i18n.Nf("%d entry", "%d entries", c.Count, c.Count))
		item := indexItem("cat:"+c.Name, &row.Widget, a.picked && c.Name == a.category, &a.onButton)
		gtkx.Append(&a.categoryList, &item.Widget)
	}
	a.categoryTitle.SetText(a.categoryName(a.category))
	a.moveCategory.SetSensitive(a.picked && a.category != "" && a.category != a.keyring)
	a.moveCategory.SetTooltipText(i18n.T("Rename or move this category"))
	if a.category == a.keyring {
		a.moveCategory.SetTooltipText(i18n.T("inua-keyring's folder: its index names these entries by path"))
	}
	a.newEntry.SetSensitive(a.store.Dir != "")
}

// showEntries lists the chosen category's entries, or, while the filter
// holds text, the entries of every category that match it.
func (a *passApp) showEntries() {
	gtkx.Clear(&a.entryList)
	q := strings.ToLower(strings.TrimSpace(a.filter.GetText()))
	for _, e := range a.entries {
		cat, sub := pass.Split(e)
		var row *gtk.Box
		switch {
		case q != "" && strings.Contains(strings.ToLower(e), q):
			row = text(sub, a.categoryName(cat))
		case q == "" && a.picked && cat == a.category:
			row = text(sub, "")
		default:
			continue
		}
		item := indexItem("entry:"+e, &row.Widget, e == a.selected, &a.onButton)
		gtkx.Append(&a.entryList, &item.Widget)
	}
}

// --- the entry ---

func (a *passApp) showEmpty(msg string) {
	a.emptyText.SetText(msg)
	a.detail.SetVisibleChildName("empty")
}

// closeEntry forgets the open entry and empties what showed it.
func (a *passApp) closeEntry() {
	a.gen++
	a.selected, a.raw, a.entry, a.otp = "", "", pass.Entry{}, nil
	a.otpCode, a.otpLeft = nil, nil
	clear(a.reveal)
	clear(a.values)
	gtkx.Clear(&a.fields)
	a.editor.GetBuffer().SetText("", 0)
}

// openEntry decrypts an entry and shows it; gpg-agent may ask for the
// passphrase meanwhile, in inua's dialog.
func (a *passApp) openEntry(name string) {
	a.closeEntry()
	a.selected = name
	a.showEntries()
	cat, sub := pass.Split(name)
	a.entryTitle.SetText(sub)
	a.entryPath.SetText(name)
	a.entryState.SetText(i18n.T("Decrypting…"))
	a.entryState.SetVisible(true)
	inKeyring := cat == a.keyring
	a.moveEntry.SetSensitive(!inKeyring)
	a.deleteEntry.SetSensitive(!inKeyring)
	a.detail.SetVisibleChildName("entry")
	gen, s := a.gen, a.store
	go func() {
		text, err := s.Show(name)
		gtkx.Post(func() {
			if gen != a.gen {
				return // another entry since
			}
			if err != nil {
				a.entryState.SetText(i18n.Tf("Cannot decrypt it: %s", err.Error()))
				return
			}
			a.entryState.SetVisible(false)
			a.raw = text
			a.entry = pass.Parse(text)
			a.otp = nil
			if a.entry.OTP != "" {
				if o, err := pass.ParseOTP(a.entry.OTP); err == nil {
					a.otp = &o
				}
			}
			a.showFields()
		})
	}()
}

// showFields shows the open entry's values, each with its copy button;
// the secret ones as dots, with a button to show them.
func (a *passApp) showFields() {
	gtkx.Clear(&a.fields)
	clear(a.values)
	a.otpCode, a.otpLeft = nil, nil
	e := a.entry
	a.addRow("password", i18n.T("PASSWORD"), e.Password, true)
	if e.OTP != "" {
		if a.otp != nil {
			a.addOTPRow()
		} else {
			a.addRow("otp", i18n.T("ONE-TIME CODE"), e.OTP, true)
		}
	}
	for i, f := range e.Fields {
		a.addRow("field:"+strconv.Itoa(i), f.Key, f.Value, f.Secret)
	}
	if e.Notes != "" {
		a.addRow("notes", i18n.T("NOTES"), e.Notes, false)
	}
}

// addRow adds a value's row: its name over it, then its buttons.
func (a *passApp) addRow(id, key, value string, secret bool) {
	a.values[id] = value
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("pass-row")
	col := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	col.SetHexpand(true)
	k := gtk.NewLabel(key)
	k.AddCssClass("kicker")
	k.SetXalign(0)
	gtkx.Append(col, &k.Widget)
	shown := !secret || a.reveal[id]
	v := gtk.NewLabel(value)
	v.AddCssClass("pass-value")
	if !shown {
		v.SetText(passMasked)
		v.AddCssClass("pass-masked")
	}
	v.SetXalign(0)
	v.SetWrap(true)
	v.SetWrapMode(2) // word or character: long tokens break anywhere
	v.SetSelectable(shown)
	gtkx.Append(col, &v.Widget)
	gtkx.Append(row, &col.Widget)
	if secret {
		icon, tip := "view-reveal-symbolic", i18n.T("Show")
		if shown {
			icon, tip = "view-conceal-symbolic", i18n.T("Hide")
		}
		gtkx.Append(row, &plainIcon("reveal:"+id, icon, tip, &a.onButton).Widget)
	}
	gtkx.Append(row, &plainIcon("copy:"+id, "edit-copy-symbolic", i18n.T("Copy"), &a.onButton).Widget)
	gtkx.Append(&a.fields, &row.Widget)
}

// addOTPRow adds the one-time code, with the seconds it stays.
func (a *passApp) addOTPRow() {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("pass-row")
	col := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	col.SetHexpand(true)
	k := gtk.NewLabel(i18n.T("ONE-TIME CODE"))
	k.AddCssClass("kicker")
	k.SetXalign(0)
	gtkx.Append(col, &k.Widget)
	line := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	a.otpCode = gtk.NewLabel("")
	a.otpCode.AddCssClass("pass-otp")
	a.otpCode.SetXalign(0)
	gtkx.Append(line, &a.otpCode.Widget)
	a.otpLeft = gtk.NewLabel("")
	a.otpLeft.AddCssClass("sub")
	a.otpLeft.SetValign(gtk.AlignCenterValue)
	gtkx.Append(line, &a.otpLeft.Widget)
	gtkx.Append(col, &line.Widget)
	gtkx.Append(row, &col.Widget)
	gtkx.Append(row, &plainIcon("copy:otp", "edit-copy-symbolic", i18n.T("Copy"), &a.onButton).Widget)
	gtkx.Append(&a.fields, &row.Widget)
	a.tickOTP()
}

// tickOTP shows the code now and the seconds it stays.
func (a *passApp) tickOTP() {
	if a.otp == nil || a.otpCode == nil {
		return
	}
	code, left := a.otp.Code(time.Now())
	a.values["otp"] = code
	a.otpCode.SetText(code[:len(code)/2] + " " + code[len(code)/2:])
	secs := int(left.Round(time.Second) / time.Second)
	a.otpLeft.SetText(i18n.Nf("%d second left", "%d seconds left", secs, secs))
}

// --- buttons ---

func (a *passApp) button(name string) {
	switch kind, arg, _ := strings.Cut(name, ":"); kind {
	case "cat":
		a.picked, a.category = true, arg
		a.filter.SetText("")
		a.showCategories()
		a.showEntries()
		if c, _ := pass.Split(a.selected); a.selected == "" || c != arg {
			a.closeEntry()
			a.showEmpty(i18n.T("Choose an entry."))
		}
	case "entry":
		if a.form == "" || a.detail.GetVisibleChildName() != "form" {
			a.openEntry(arg)
		}
	case "reveal":
		a.reveal[arg] = !a.reveal[arg]
		a.showFields()
	case "copy":
		a.copySecret(a.values[arg])
	case "add-store":
		a.chooseStore()
	case "move-category":
		a.openForm(formMoveCategory, a.category)
	case "move-entry":
		a.openForm(formMoveEntry, a.selected)
	case "new-entry":
		a.openForm(formNew, "")
	case "delete":
		a.openForm(formDelete, a.selected)
	case "edit":
		a.openEditor(a.selected, a.raw)
	case "edit-cancel":
		a.closeEditor()
	case "edit-save":
		a.saveEditor()
	case "form-cancel":
		a.closeForm()
	case "form-ok":
		a.submitForm()
	}
}

// copySecret puts a value on the clipboard with the password manager's
// hint, which clipboard histories (inua's among them) leave out, and
// takes it off again after a while if it is still there.
func (a *passApp) copySecret(value string) {
	if value == "" {
		return
	}
	clip := a.win.GetClipboard()
	defer clip.Unref()
	data := []byte(value)
	providers := []uintptr{
		gdk.NewContentProviderForBytes("text/plain;charset=utf-8", glib.NewBytes(data, uint(len(data)))).GoPointer(),
		gdk.NewContentProviderForBytes("text/plain", glib.NewBytes(data, uint(len(data)))).GoPointer(),
		gdk.NewContentProviderForBytes("x-kde-passwordManagerHint", glib.NewBytes([]byte("secret"), 6)).GoPointer(),
	}
	union := gdk.NewContentProviderUnion(uintptr(unsafe.Pointer(&providers[0])), uint(len(providers)))
	clip.SetContent(union)
	union.Unref()
	a.clipGen++
	gen := a.clipGen
	a.onClear = func(uintptr) bool {
		if gen == a.clipGen {
			c := a.win.GetClipboard()
			if c.IsLocal() { // still ours: nothing else was copied since
				c.SetContent(&gdk.ContentProvider{})
			}
			c.Unref()
		}
		return false
	}
	glib.TimeoutAdd(uint32(passClearAfter/time.Millisecond), &a.onClear, 0)
}

// --- the editor ---

func (a *passApp) openEditor(name, content string) {
	a.editTitle.SetText(name)
	a.editor.GetBuffer().SetText(content, -1)
	a.editError.SetVisible(false)
	a.detail.SetVisibleChildName("edit")
	a.editor.GrabFocus()
}

func (a *passApp) closeEditor() {
	a.editor.GetBuffer().SetText("", 0)
	if a.editNew {
		a.editNew = false
		a.showEmpty(i18n.T("Choose an entry."))
		return
	}
	a.detail.SetVisibleChildName("entry")
}

func (a *passApp) saveEditor() {
	if a.busy {
		return
	}
	buf := a.editor.GetBuffer()
	var start, end gtk.TextIter
	buf.GetBounds(&start, &end)
	content := buf.GetText(&start, &end, true)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	name := a.selected
	if a.editNew {
		name = a.newName
	}
	a.busy = true
	s := a.store
	go func() {
		err := s.Insert(name, content)
		gtkx.Post(func() {
			a.busy = false
			if err != nil {
				a.editError.SetText(err.Error())
				a.editError.SetVisible(true)
				return
			}
			a.editNew = false
			a.editor.GetBuffer().SetText("", 0)
			a.reload(name)
		})
	}()
}

// --- the form ---

// openForm shows the form that renames or moves a category or an entry,
// names a new entry, or asks before deleting one.
func (a *passApp) openForm(form, from string) {
	a.form, a.formFrom = form, from
	a.filling = true
	cats := []string{}
	for _, c := range pass.Categories(a.entries) {
		if c.Name != "" {
			cats = append(cats, c.Name)
		}
	}
	a.categories = cats
	a.formCategories.SetModel(gtk.NewStringList(append([]string{i18n.T("Categories…")}, cats...)))
	a.formCategories.SetSelected(0)
	a.filling = false
	a.formCategoryBox.SetVisible(form == formMoveEntry || form == formNew)
	a.formSubBox.SetVisible(form != formDelete)
	a.formOK.RemoveCssClass("destructive-action")
	a.formOK.AddCssClass("suggested-action")
	switch form {
	case formMoveCategory:
		a.formTitle.SetText(i18n.Tf("Rename or move “%s”", from))
		a.formText.SetText(i18n.T("Its new path: another name renames it; a path under another category (archive/work) moves it there."))
		a.formSub.SetText(from)
		a.formOK.SetLabel(i18n.T("Move"))
	case formMoveEntry:
		cat, sub := pass.Split(from)
		a.formTitle.SetText(i18n.Tf("Rename or move “%s”", sub))
		a.formText.SetText(i18n.T("The same category and a new name renames it; another category moves it."))
		a.formCategory.SetText(cat)
		a.formSub.SetText(sub)
		a.formOK.SetLabel(i18n.T("Move"))
	case formNew:
		a.formTitle.SetText(i18n.T("New entry"))
		a.formText.SetText(i18n.T("Where it goes; then its content."))
		cat := ""
		if a.picked {
			cat = a.category
		}
		a.formCategory.SetText(cat)
		a.formSub.SetText("")
		a.formOK.SetLabel(i18n.T("Next"))
	case formDelete:
		a.formTitle.SetText(i18n.Tf("Delete “%s”?", from))
		a.formText.SetText(i18n.T("pass removes its file; a git store keeps it in its history."))
		a.formOK.SetLabel(i18n.T("Delete"))
		a.formOK.RemoveCssClass("suggested-action")
		a.formOK.AddCssClass("destructive-action")
	}
	a.checkForm()
	a.detail.SetVisibleChildName("form")
	if form == formNew {
		a.formSub.GrabFocus()
	} else if form != formDelete {
		a.formSub.GrabFocus()
	}
}

// formTarget is where the form would put what it moves or makes.
func (a *passApp) formTarget() string {
	sub := strings.Trim(strings.TrimSpace(a.formSub.GetText()), "/")
	switch a.form {
	case formMoveCategory:
		return sub
	case formMoveEntry, formNew:
		return pass.Join(strings.Trim(strings.TrimSpace(a.formCategory.GetText()), "/"), sub)
	}
	return a.formFrom
}

// checkForm says where it would end up, or why it cannot, and allows the
// action only when it can.
func (a *passApp) checkForm() {
	if a.form == "" || a.filling {
		return
	}
	to := a.formTarget()
	ok, msg := true, ""
	switch {
	case a.form == formDelete:
		msg = a.store.Dir + "/" + a.formFrom + ".gpg"
	case pass.ValidName(to) != nil:
		ok, msg = false, i18n.T("Each part of the path needs a name, without a leading dot.")
	case to == a.formFrom:
		ok, msg = false, i18n.T("That is where it is.")
	case a.store.Has(to):
		ok, msg = false, i18n.Tf("%s is already in the store.", to)
	case a.form == formMoveCategory && strings.HasPrefix(to, a.formFrom+"/"):
		ok, msg = false, i18n.T("A category cannot move into itself.")
	default:
		msg = i18n.Tf("It will be %s", to)
	}
	a.formResult.SetText(msg)
	setClass(&a.formResult.Widget, "pass-error", !ok)
	a.formOK.SetSensitive(ok && !a.busy)
}

func (a *passApp) closeForm() {
	form := a.form
	a.form = ""
	switch {
	case form == formNew || a.selected == "":
		a.showEmpty(i18n.T("Choose an entry."))
	default:
		a.detail.SetVisibleChildName("entry")
	}
}

func (a *passApp) submitForm() {
	if a.busy || !a.formOK.GetSensitive() {
		return
	}
	form, from, to := a.form, a.formFrom, a.formTarget()
	if form == formNew {
		a.form, a.editNew, a.newName = "", true, to
		a.closeEntry()
		a.openEditor(to, "")
		return
	}
	a.busy = true
	a.formOK.SetSensitive(false)
	s := a.store
	go func() {
		var err error
		if form == formDelete {
			err = s.Remove(from)
		} else {
			err = s.Move(from, to)
		}
		gtkx.Post(func() {
			a.busy = false
			if err != nil {
				a.formResult.SetText(err.Error())
				setClass(&a.formResult.Widget, "pass-error", true)
				a.formOK.SetSensitive(true)
				return
			}
			a.form = ""
			switch form {
			case formDelete:
				a.closeEntry()
				a.reload("")
			case formMoveCategory:
				a.category, a.picked = to, true
				if c, _ := pass.Split(to); c != to {
					a.category = c
				}
				open := ""
				if a.selected != "" {
					if c, sub := pass.Split(a.selected); c == from {
						open = to + "/" + sub
					}
				}
				a.closeEntry()
				a.reload(open)
				if open == "" {
					a.showEmpty(i18n.T("Choose an entry."))
				}
			default:
				a.closeEntry()
				a.reload(to)
			}
		})
	}()
}
