package web

import "github.com/lyricat/inua/internal/i18n"

// pageText is the text the page shows (static/index.html), in English:
// T("…"), Tf("…") and data-t="…" there. The page gets it in the user's
// language (strings); a test keeps the two lists the same.
var pageText = []string{
	i18n.M("%d KB"),
	i18n.M("%d MS"),
	i18n.M("%dd"),
	i18n.M("%dd %dh"),
	i18n.M("%dh"),
	i18n.M("%dh %dm"),
	i18n.M("%dm"),
	i18n.M("%s MB"),
	i18n.M("%s PX"),
	i18n.M("%s px"),
	i18n.M("AS INUA-TERM"),
	i18n.M("Add a snippet"),
	i18n.M("Agents"),
	i18n.M("As inua-term"),
	i18n.M("Asking…"),
	i18n.M("BAR"),
	i18n.M("BLOCK"),
	i18n.M("Back"),
	i18n.M("Basic"),
	i18n.M("Built in"),
	i18n.M("CONNECTED"),
	i18n.M("CONNECTING…"),
	i18n.M("Cancel"),
	i18n.M("Close"),
	i18n.M("Columns × rows: the pane fitted to this screen at the text size."),
	i18n.M("Connection"),
	i18n.M("Continue."),
	i18n.M("Control"),
	i18n.M("Copied"),
	i18n.M("Copy"),
	i18n.M("Create"),
	i18n.M("Cursor"),
	i18n.M("Cursor blinks"),
	i18n.M("DONE"),
	i18n.M("Delete"),
	i18n.M("Drawing"),
	i18n.M("Drawn by the browser's page: slower, used when WebGL is missing."),
	i18n.M("Drawn by the graphics chip: the fast way."),
	i18n.M("Esc, Tab, Ctrl, the arrows and the rest a touch keyboard lacks."),
	i18n.M("Everything from this pane since it opened here."),
	i18n.M("Explain it first, before you change anything."),
	i18n.M("First load"),
	i18n.M("Font"),
	i18n.M("Function"),
	i18n.M("Host"),
	i18n.M("IDLE"),
	i18n.M("INFO"),
	i18n.M("KEYS"),
	i18n.M("Key panel"),
	i18n.M("Keyboard"),
	i18n.M("Keys above the keyboard"),
	i18n.M("Move"),
	i18n.M("NEEDS YOU"),
	i18n.M("New pane"),
	i18n.M("New session"),
	i18n.M("New snippet"),
	i18n.M("New workspace"),
	i18n.M("New workspace, in the folder"),
	i18n.M("No"),
	i18n.M("No agent is running."),
	i18n.M("No pane is open. Choose one in Agents or Workspaces."),
	i18n.M("No."),
	i18n.M("Not copied"),
	i18n.M("OFFLINE"),
	i18n.M("OFFLINE · RECONNECTING"),
	i18n.M("OK"),
	i18n.M("Overview"),
	i18n.M("PLAIN"),
	i18n.M("Panes are fitted to this screen at this size; pinching changes it too."),
	i18n.M("Pinch, or Settings in Overview, to change it."),
	i18n.M("RESTORED"),
	i18n.M("Received"),
	i18n.M("Rename"),
	i18n.M("Rename the session"),
	i18n.M("Round trip"),
	i18n.M("SNIPPETS"),
	i18n.M("Save"),
	i18n.M("Select all"),
	i18n.M("Send"),
	i18n.M("Session %d"),
	i18n.M("Settings"),
	i18n.M("Shell"),
	i18n.M("Size"),
	i18n.M("Sure, please do it."),
	i18n.M("Symbols"),
	i18n.M("Terminal"),
	i18n.M("Terminal colours"),
	i18n.M("Text size"),
	i18n.M("The pane's recent output as it came, and how long it took to arrive and draw."),
	i18n.M("The pane's screen and history as the computer has them, and how long they took to arrive and draw."),
	i18n.M("The theme's"),
	i18n.M("The window theme's ground and text"),
	i18n.M("Theme"),
	i18n.M("This computer"),
	i18n.M("This device"),
	i18n.M("To the bottom"),
	i18n.M("UNDERLINE"),
	i18n.M("WEBGL"),
	i18n.M("WORKING"),
	i18n.M("Wait, let me think."),
	i18n.M("Workspace"),
	i18n.M("Workspaces"),
	i18n.M("Yes."),
	i18n.M("Yours"),
	i18n.M("gpg asks for your passphrase"),
	i18n.M("now"),
	i18n.M("since %s"),
	i18n.M("the theme's"),
	i18n.M("the theme's colours"),
}

// pagePlurals are the page's text by number (Nf there): the form for one,
// the form for many.
var pagePlurals = [][2]string{
	{"%d pane", "%d panes"},
	{"%d session", "%d sessions"},
}

// pageStrings is the page's text in the user's language: by its English, the
// text; and the forms by number, by the form for one.
func pageStrings() (text, one, many map[string]string) {
	text = make(map[string]string, len(pageText))
	for _, s := range pageText {
		text[s] = i18n.T(s)
	}
	one, many = map[string]string{}, map[string]string{}
	for _, p := range pagePlurals {
		one[p[0]] = i18n.N(p[0], p[1], 1)
		many[p[0]] = i18n.N(p[0], p[1], 2)
	}
	return text, one, many
}
