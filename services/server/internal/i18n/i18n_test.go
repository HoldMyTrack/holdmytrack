package i18n

import (
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var pluralSuffix = regexp.MustCompile(`\.(one|few|many|other)$`)

// forms is each plural form a language's rule can pick.
var forms = map[string][]string{"en": {"one", "other"}, "ru": {"one", "few", "many"}}

// TestCatalogParity: every language has every message English has, in each plural form its
// own rule uses, with the same {placeholders} — and nothing English doesn't.
func TestCatalogParity(t *testing.T) {
	type entry struct{ plural bool }
	base := map[string]entry{}
	for _, k := range Keys(Default) {
		b := pluralSuffix.ReplaceAllString(k, "")
		base[b] = entry{plural: b != k}
	}
	for _, lang := range Supported {
		keys := Keys(lang)
		for b, e := range base {
			want := []string{b}
			if e.plural {
				want = nil
				for _, f := range forms[lang] {
					want = append(want, b+"."+f)
				}
			}
			for _, k := range want {
				msg, ok := Raw(lang, k)
				if !ok {
					t.Errorf("%s: missing %q", lang, k)
					continue
				}
				enKey := k
				if e.plural {
					enKey = b + ".other"
				}
				en, _ := Raw(Default, enKey)
				if got, want := placeholders(msg), placeholders(en); !slices.Equal(got, want) {
					t.Errorf("%s: %q has placeholders %v, English %v", lang, k, got, want)
				}
			}
		}
		for _, k := range keys {
			if _, ok := base[pluralSuffix.ReplaceAllString(k, "")]; !ok {
				t.Errorf("%s: %q isn't in the English catalog", lang, k)
			}
		}
	}
}

func placeholders(msg string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`\{[a-z_]+\}`).FindAllString(msg, -1) {
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	slices.Sort(out)
	return out
}

func TestPluralForm(t *testing.T) {
	for n, want := range map[int64]string{0: "many", 1: "one", 2: "few", 4: "few", 5: "many", 11: "many", 12: "many", 14: "many", 21: "one", 22: "few", 25: "many", 101: "one", 111: "many", 112: "many", 122: "few"} {
		if got := PluralForm("ru", n); got != want {
			t.Errorf("ru %d: %s, want %s", n, got, want)
		}
	}
	for n, want := range map[int64]string{0: "other", 1: "one", 2: "other", 21: "other"} {
		if got := PluralForm("en", n); got != want {
			t.Errorf("en %d: %s, want %s", n, got, want)
		}
	}
}

func TestFormatting(t *testing.T) {
	en, ru := Get("en"), Get("ru")
	for got, want := range map[string]string{
		en.N("count.days", 1):                         "1 day",
		en.N("count.days", 1500):                      "1,500 days",
		ru.N("count.days", 1):                         "1 день",
		ru.N("count.days", 3):                         "3 дня",
		ru.N("count.days", 1500):                      "1 500 дней",
		en.T("error.password_too_short", "min", 8):    "Password must be at least 8 characters.",
		en.Int(-1234):                                 "-1,234",
		en.Int(12):                                    "12",
		en.Float(1234.56, 1):                          "1,234.6",
		ru.Float(1234.56, 1):                          "1 234,6",
		en.Float(-0.04, 1):                            "0.0",
		en.Float(-2.5, 1):                             "-2.5",
		en.T("no.such.key"):                           "no.such.key",
		Get("xx").T("header.profile"):                 "Profile",
		ru.T("meta.title", "page", "Вход"):            "Вход — HoldMyTrack",
		en.T("verify_pending.hint", "email", "a@b.c"): "We sent a confirmation link to <strong>a@b.c</strong>. Click it to unlock your account.",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestNegotiate(t *testing.T) {
	for header, want := range map[string]string{
		"":                                    "",
		"ru-RU,ru;q=0.9,en-US;q=0.8":          "ru",
		"en-GB,en;q=0.9,ru;q=0.8":             "en",
		"de-DE,de;q=0.9":                      "",
		"de-DE,ru;q=0.5,en;q=0.4":             "ru",
		"en;q=0.3, RU;q=0.7":                  "ru",
		"ru;q=bogus, en":                      "en",
		"*":                                   "",
		"uk-UA, uk;q=0.9, ru;q=0.8, en;q=0.5": "ru",
		"ru;q=0.8, en;":                       "en",
	} {
		if got := Negotiate(header); got != want {
			t.Errorf("Negotiate(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestResolve(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "ru")
	if got := Resolve("en", r); got != "en" {
		t.Errorf("account setting should win: %s", got)
	}
	if got := Resolve("", r); got != "ru" {
		t.Errorf("browser should decide an automatic account: %s", got)
	}
	if got := Resolve("xx", nil); got != Default {
		t.Errorf("unknown setting, no request: %s", got)
	}
}

// TestCatalogsAreClean catches what a hand-edited catalog gets wrong: stray whitespace, an
// empty message, and an unbalanced placeholder brace.
func TestCatalogsAreClean(t *testing.T) {
	for _, lang := range Supported {
		for _, k := range Keys(lang) {
			msg, _ := Raw(lang, k)
			if msg == "" || strings.TrimSpace(msg) != msg {
				t.Errorf("%s %q: empty or padded message", lang, k)
			}
			if strings.Count(msg, "{") != strings.Count(msg, "}") {
				t.Errorf("%s %q: unbalanced braces", lang, k)
			}
		}
	}
}
