// Package i18n is the server's localization (ADR-0014, IMPLEMENTATION.md §4.21): one flat
// catalog per language (locales/*.json, embedded), which language a request gets, and the
// number formatting that differs between them. No dependency — two languages' plural rules
// and separators are a few lines each, and a message format library would be most of the code
// this package replaces.
//
// A catalog maps a key to a message. {name} in a message is replaced by the argument of that
// name; a count's message is four keys, key.one/.few/.many/.other, of which a language uses the
// forms its plural rule picks (English one/other, Russian one/few/many). English is the source
// of truth: a key missing from another catalog falls back to English, and one missing there
// renders as the key itself — which the tests look for.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

//go:embed locales/*.json
var files embed.FS

// Default is the language anyone gets when nothing asks for another.
const Default = "en"

// Supported are the languages with a catalog, in the order Settings lists them.
var Supported = []string{"en", "ru"}

// Names are each language's name in itself — what Settings' Language list shows, never
// translated, so someone can find their own language whatever the page is in.
var Names = map[string]string{"en": "English", "ru": "Русский"}

// Localizer is one language's catalog and formatting.
type Localizer struct {
	lang     string
	msgs     map[string]string
	fallback *Localizer
}

var catalogs = load()

func load() map[string]*Localizer {
	out := make(map[string]*Localizer, len(Supported))
	for _, lang := range Supported {
		raw, err := files.ReadFile("locales/" + lang + ".json")
		if err != nil {
			panic(fmt.Sprintf("i18n: %s: %v", lang, err))
		}
		l := &Localizer{lang: lang}
		if err := json.Unmarshal(raw, &l.msgs); err != nil {
			panic(fmt.Sprintf("i18n: %s: %v", lang, err))
		}
		out[lang] = l
	}
	for lang, l := range out {
		if lang != Default {
			l.fallback = out[Default]
		}
	}
	return out
}

// Get is lang's Localizer, or English's for a language without a catalog.
func Get(lang string) *Localizer {
	if l, ok := catalogs[lang]; ok {
		return l
	}
	return catalogs[Default]
}

// IsSupported reports whether lang has a catalog.
func IsSupported(lang string) bool {
	_, ok := catalogs[lang]
	return ok
}

// Keys are every key in lang's catalog, sorted — for the tests' parity checks.
func Keys(lang string) []string {
	l, ok := catalogs[lang]
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(l.msgs))
	for k := range l.msgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Raw is key's message in lang's own catalog, unformatted, and whether it has one.
func Raw(lang, key string) (string, bool) {
	l, ok := catalogs[lang]
	if !ok {
		return "", false
	}
	msg, ok := l.msgs[key]
	return msg, ok
}

// Lang is the language's code, for <html lang>.
func (l *Localizer) Lang() string { return l.lang }

func (l *Localizer) lookup(key string) string {
	for c := l; c != nil; c = c.fallback {
		if msg, ok := c.msgs[key]; ok {
			return msg
		}
	}
	return key
}

// T is key's message with each {name} replaced — args are name/value pairs. A value that is an
// integer is formatted with this language's thousands separators.
func (l *Localizer) T(key string, args ...any) string {
	return l.format(l.lookup(key), args)
}

// N is the plural form of key that fits n — key.one, key.few, … — with {n} set to n, formatted,
// alongside any other name/value pairs in args.
func (l *Localizer) N(key string, n int64, args ...any) string {
	return l.format(l.lookup(key+"."+PluralForm(l.lang, n)), append([]any{"n", n}, args...))
}

func (l *Localizer) format(msg string, args []any) string {
	if len(args) == 0 || !strings.Contains(msg, "{") {
		return msg
	}
	pairs := make([]string, 0, len(args))
	for i := 0; i+1 < len(args); i += 2 {
		name, _ := args[i].(string)
		pairs = append(pairs, "{"+name+"}", l.value(args[i+1]))
	}
	return strings.NewReplacer(pairs...).Replace(msg)
}

func (l *Localizer) value(v any) string {
	switch n := v.(type) {
	case int:
		return l.Int(int64(n))
	case int64:
		return l.Int(n)
	case string:
		return n
	}
	return fmt.Sprint(v)
}

// PluralForm is CLDR's cardinal category for an integer n in lang: "one" or "other" in
// English; "one" (1, 21, 101…), "few" (2–4, 22–24…) or "many" (0, 5–20, 25–30…) in Russian.
func PluralForm(lang string, n int64) string {
	if n < 0 {
		n = -n
	}
	switch lang {
	case "ru":
		mod10, mod100 := n%10, n%100
		switch {
		case mod10 == 1 && mod100 != 11:
			return "one"
		case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
			return "few"
		}
		return "many"
	}
	if n == 1 {
		return "one"
	}
	return "other"
}

// separators are a language's thousands and decimal separators — Russian's thousands one is a
// no-break space, as CLDR and browsers' Intl.NumberFormat have it.
func (l *Localizer) separators() (group, decimal string) {
	if l.lang == "ru" {
		return " ", ","
	}
	return ",", "."
}

// Int is n with thousands separators: 1234567 → "1,234,567" / "1 234 567".
func (l *Localizer) Int(n int64) string {
	group, _ := l.separators()
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString(group)
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// Float is v with exactly `decimals` digits after the decimal separator, and thousands
// separators before it: 1234.56, 1 → "1,234.6" / "1 234,6".
func (l *Localizer) Float(v float64, decimals int) string {
	_, decimal := l.separators()
	s := strconv.FormatFloat(math.Abs(v), 'f', decimals, 64)
	whole, frac, _ := strings.Cut(s, ".")
	n, _ := strconv.ParseInt(whole, 10, 64)
	out := l.Int(n)
	if v < 0 && strings.Trim(s, "0.") != "" {
		out = "-" + out
	}
	if frac != "" {
		out += decimal + frac
	}
	return out
}

// Negotiate picks the supported language an Accept-Language header prefers most — by q-value,
// then by order — matching on the primary subtag, so "ru-RU" is Russian. "" when the header
// names none of them.
func Negotiate(header string) string {
	best, bestQ := "", 0.0
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil {
				continue
			}
			q = parsed
		}
		primary, _, _ := strings.Cut(strings.ToLower(tag), "-")
		if q > bestQ && IsSupported(primary) {
			best, bestQ = primary, q
		}
	}
	return best
}

// Resolve is the language a request gets: the account's own setting when it has one, or else
// what the browser asks for, or else English.
func Resolve(accountLang string, r *http.Request) string {
	if IsSupported(accountLang) {
		return accountLang
	}
	if r != nil {
		if lang := Negotiate(r.Header.Get("Accept-Language")); lang != "" {
			return lang
		}
	}
	return Default
}
