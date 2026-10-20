package shell

import (
	"slices"
	"testing"
)

func TestSearchSettings(t *testing.T) {
	items := []SettingsItem{
		{Page: "appearance", PageTitle: "Appearance"},
		{Page: "appearance", PageTitle: "Appearance", Section: "Theme", Title: "Text size", Sub: "Every text in the shell"},
		{Page: "bar", PageTitle: "Bar & Dock"},
		{Page: "bar", PageTitle: "Bar & Dock", Section: "Bar", Title: "Clock", Sub: "strftime format"},
		{Page: "wifi", PageTitle: "Wi-Fi"},
		{Page: "wifi", PageTitle: "Wi-Fi", Section: "Wi-fi", Title: "Wi-Fi", Sub: "Connected to “Home”"},
		{Page: "bluetooth", PageTitle: "Bluetooth"},
		{Page: "bluetooth", PageTitle: "Bluetooth", Section: "My devices", Title: "WH-1000XM5", Sub: "Connected"},
		{Page: "about", PageTitle: "About"},
	}
	names := func(ix []int) []string {
		var out []string
		for _, i := range ix {
			it := items[i]
			if it.Title == "" {
				out = append(out, it.Page)
			} else {
				out = append(out, it.Page+"/"+it.Title)
			}
		}
		return out
	}
	for _, c := range []struct {
		q    string
		want []string
	}{
		// The page first, then its rows; "wifi" finds "Wi-Fi".
		{"wifi", []string{"wifi", "wifi/Wi-Fi"}},
		{"WI-FI", []string{"wifi", "wifi/Wi-Fi"}},
		// A keyword finds the page; a row's own words find the row.
		{"network", []string{"wifi"}},
		{"clock", []string{"bar/Clock", "bar"}},
		{"connected", []string{"wifi/Wi-Fi", "bluetooth/WH-1000XM5"}},
		// Every word must be there, in any field.
		{"text every", []string{"appearance/Text size"}},
		{"text dock", nil},
		{"neofetch", []string{"about"}},
		{"   ", nil},
	} {
		if got := names(SearchSettings(items, c.q)); !slices.Equal(got, c.want) {
			t.Errorf("SearchSettings(%q) = %q, want %q", c.q, got, c.want)
		}
	}
}

func TestSectionName(t *testing.T) {
	for in, want := range map[string]string{"[MY DEVICES]": "My devices", "WI-FI": "Wi-fi", "[]": "", "[ディスプレイ]": "ディスプレイ"} {
		if got := SectionName(in); got != want {
			t.Errorf("SectionName(%q) = %q, want %q", in, got, want)
		}
	}
}
