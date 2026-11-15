package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/arqueon/dux-mail/core/models"
)

func TestParseColorPair(t *testing.T) {
	bg, text, err := parseColorPair("#16A765:#FFFFFF", false)
	if err != nil || bg != "#16a765" || text != "#ffffff" {
		t.Fatalf("got %q %q %v", bg, text, err)
	}
	if bg, _, err := parseColorPair("NONE", true); err != nil || bg != "none" {
		t.Fatalf("none: %q %v", bg, err)
	}
	for _, bad := range []string{"", "#16a765", "16a765:ffffff", "#16a765:#fff", "none", "#zzzzzz:#ffffff"} {
		if _, _, err := parseColorPair(bad, false); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestPickLabelByIDNameAndAmbiguity(t *testing.T) {
	labels := []models.LabelView{
		{ID: "Label_1", AccountID: "A", Name: "Auto/Compras"},
		{ID: "Label_9", AccountID: "B", Name: "auto/compras"},
		{ID: "Label_2", AccountID: "A", Name: "Work"},
	}
	emails := map[string]string{"A": "a@example.org", "B": "b@example.org"}

	if l, err := pickLabel(labels, emails, "WORK"); err != nil || l.ID != "Label_2" {
		t.Errorf("case-insensitive name: %+v %v", l, err)
	}
	if l, err := pickLabel(labels, emails, "Label_9"); err != nil || l.AccountID != "B" {
		t.Errorf("by ID: %+v %v", l, err)
	}
	_, err := pickLabel(labels, emails, "Auto/Compras")
	if err == nil || !strings.Contains(err.Error(), "--account") || !strings.Contains(err.Error(), "b@example.org") {
		t.Errorf("ambiguous name error = %v", err)
	}
	if _, err := pickLabel(labels, emails, "nope"); err == nil {
		t.Error("unknown label must fail")
	}
}

func TestNewLabelEnvFiltersCapabilityAndAccount(t *testing.T) {
	all := []models.AccountView{
		{ID: "A", Email: "a@example.org", Capabilities: []string{"labels"}},
		{ID: "B", Email: "b@example.org", Capabilities: []string{"labels"}},
		{ID: "I", Email: "i@example.org", Capabilities: []string{}},
	}
	env, err := newLabelEnv(all, "")
	if err != nil || len(env.accounts) != 2 || env.account != nil {
		t.Fatalf("env = %+v %v", env, err)
	}
	if _, err := env.accountForCreate(); err == nil {
		t.Error("creating with two label accounts and no --account must fail")
	}
	env, err = newLabelEnv(all, "B@example.org")
	if err != nil || env.account == nil || env.account.ID != "B" {
		t.Fatalf("by email: %+v %v", env, err)
	}
	if _, err := newLabelEnv(all, "i@example.org"); err == nil {
		t.Error("an account without labels must not be selectable")
	}
	if _, err := newLabelEnv(all[2:], ""); err == nil {
		t.Error("no label-capable account must be an error")
	}
	one, _ := newLabelEnv(all[:1], "")
	if acct, err := one.accountForCreate(); err != nil || acct.ID != "A" {
		t.Errorf("single account default: %v %v", acct, err)
	}
}

func TestConfirmLabelDeleteAndThreadIDs(t *testing.T) {
	l := models.LabelView{Name: "Auto/Compras"}
	for in, want := range map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "\n": false, "": false} {
		var out bytes.Buffer
		if got := confirmLabelDelete(strings.NewReader(in), &out, l, "a@example.org"); got != want {
			t.Errorf("input %q: got %v want %v", in, got, want)
		}
		if !strings.Contains(out.String(), "Auto/Compras") {
			t.Errorf("prompt does not name the label: %q", out.String())
		}
	}
	if _, err := parseThreadIDs([]string{"12", "x"}); err == nil {
		t.Error("non-numeric thread id must fail")
	}
	if ids, err := parseThreadIDs([]string{"12", "3"}); err != nil || len(ids) != 2 {
		t.Errorf("ids = %v %v", ids, err)
	}
}
