package alerts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/shim"
)

const (
	colorRed    = 0xED4245
	colorGreen  = 0x57F287
	colorYellow = 0xFEE75C
	colorOrange = 0xE67E22
	colorBlue   = 0x5865F2
	logLines    = 15
	logChars    = 1000
)

type msgTexts struct {
	downContent, downTitle, downBody, downLoop string
	cleanExit, crashed                         string
	recoveredTitle, recoveredBody              string
	restartedTitle, restartedBody              string
	errorTitle                                 string
	fieldExit, fieldCrashes, fieldLog          string
	open                                       string
	testContent, testTitle, testBody           string
}

var textsPL = msgTexts{
	downContent:    "🔴 **%s** nie działa!",
	downTitle:      "%s nie działa",
	downBody:       "%s <t:%d:T> i nie uruchomił się ponownie w ciągu %d s.",
	downLoop:       "Padł %d razy z rzędu od <t:%d:T> i nie potrafi wystartować.",
	cleanExit:      "Wyłączył się sam",
	crashed:        "Padł",
	recoveredTitle: "%s znowu działa",
	recoveredBody:  "Przerwa trwała %s.",
	restartedTitle: "%s padł, ale wstał",
	restartedBody:  "Wyłączył się <t:%d:T> i uruchomił się ponownie automatycznie.",
	errorTitle:     "Błąd w konsoli: %s",
	fieldExit:      "Kod wyjścia",
	fieldCrashes:   "Ile razy padł",
	fieldLog:       "Ostatnie linie logu",
	open:           "Otwórz w panelu",
	testContent:    "Test powiadomień panel.cypek.dev",
	testTitle:      "Powiadomienia działają",
	testBody:       "Tak będą wyglądać alerty, gdy serwer padnie i nie wstanie.",
}

var textsEN = msgTexts{
	downContent:    "🔴 **%s** is down!",
	downTitle:      "%s is down",
	downBody:       "%s at <t:%d:T> and did not come back within %d s.",
	downLoop:       "Crashed %d times in a row since <t:%d:T> and cannot start.",
	cleanExit:      "It exited on its own",
	crashed:        "It crashed",
	recoveredTitle: "%s is back up",
	recoveredBody:  "It was down for %s.",
	restartedTitle: "%s crashed but recovered",
	restartedBody:  "It stopped at <t:%d:T> and was restarted automatically.",
	errorTitle:     "Console error: %s",
	fieldExit:      "Exit code",
	fieldCrashes:   "Crashes",
	fieldLog:       "Last log lines",
	open:           "Open in panel",
	testContent:    "panel.cypek.dev alert test",
	testTitle:      "Alerts work",
	testBody:       "This is how alerts will look when a server goes down and stays down.",
}

var textsDE = msgTexts{
	downContent:    "🔴 **%s** ist ausgefallen!",
	downTitle:      "%s ist ausgefallen",
	downBody:       "%s um <t:%d:T> und lief innerhalb von %d s nicht wieder an.",
	downLoop:       "Seit <t:%[2]d:T> %[1]d-mal hintereinander abgestürzt, der Start klappt nicht.",
	cleanExit:      "Hat sich selbst beendet",
	crashed:        "Abgestürzt",
	recoveredTitle: "%s läuft wieder",
	recoveredBody:  "Der Ausfall dauerte %s.",
	restartedTitle: "%s ist abgestürzt, läuft aber wieder",
	restartedBody:  "Um <t:%d:T> beendet und automatisch neu gestartet.",
	errorTitle:     "Fehler in der Konsole: %s",
	fieldExit:      "Exit-Code",
	fieldCrashes:   "Abstürze",
	fieldLog:       "Letzte Logzeilen",
	open:           "Im Panel öffnen",
	testContent:    "panel.cypek.dev Benachrichtigungstest",
	testTitle:      "Benachrichtigungen funktionieren",
	testBody:       "So sehen Alerts aus, wenn ein Server ausfällt und nicht wieder startet.",
}

func texts(lang string) msgTexts {
	switch lang {
	case "en":
		return textsEN
	case "de":
		return textsDE
	}
	return textsPL
}

func (m *Monitor) baseEmbed(srv ServerInfo, s Settings, title string, color int) Embed {
	e := Embed{Title: title, Color: color, Timestamp: m.now().UTC().Format(time.RFC3339), Footer: &EmbedFooter{Text: "panel.cypek.dev"}}
	if s.PanelURL != "" {
		e.URL = strings.TrimRight(s.PanelURL, "/") + "/servers/" + srv.ID
	}
	return e
}

func logField(t msgTexts, srv ServerInfo, s Settings) []EmbedField {
	if !s.IncludeLogs {
		return nil
	}
	lines := LastLogLines(srv.Dir, logLines)
	if len(lines) == 0 {
		return nil
	}
	return []EmbedField{{Name: t.fieldLog, Value: codeBlock(lines)}}
}

func codeBlock(lines []string) string {
	text := strings.ReplaceAll(strings.Join(lines, "\n"), "```", "'''")
	if len(text) > logChars {
		text = text[len(text)-logChars:]
		if i := strings.IndexByte(text, '\n'); i >= 0 && i < 200 {
			text = text[i+1:]
		}
		text = strings.ToValidUTF8(text, "")
	}
	return "```\n" + text + "\n```"
}

func exitField(t msgTexts, inc *incident) []EmbedField {
	var f []EmbedField
	if inc.exitCode != nil {
		f = append(f, EmbedField{Name: t.fieldExit, Value: fmt.Sprint(*inc.exitCode), Inline: true})
	}
	if inc.crashes > 1 {
		f = append(f, EmbedField{Name: t.fieldCrashes, Value: fmt.Sprint(inc.crashes), Inline: true})
	}
	return f
}

func (m *Monitor) downMessage(srv ServerInfo, s Settings, inc *incident, mention string) Message {
	t := texts(s.Language)
	e := m.baseEmbed(srv, s, fmt.Sprintf(t.downTitle, srv.Name), colorRed)
	how := t.crashed
	if inc.kind == "exit" {
		how = t.cleanExit
	}
	if inc.crashes >= crashLoop {
		e.Description = fmt.Sprintf(t.downLoop, inc.crashes, inc.start.Unix())
	} else {
		e.Description = fmt.Sprintf(t.downBody, how, inc.last.Unix(), s.GraceSec)
	}
	if e.URL != "" {
		e.Description += fmt.Sprintf("\n[%s](%s)", t.open, e.URL)
	}
	e.Fields = append(exitField(t, inc), logField(t, srv, s)...)
	msg := Message{Content: fmt.Sprintf(t.downContent, escapeMarkdown(srv.Name)), Embeds: []Embed{e}}
	men, _ := ParseMention(mention)
	msg.withMention(men)
	return msg
}

func (m *Monitor) recoveredMessage(srv ServerInfo, s Settings, inc *incident, now time.Time) Message {
	t := texts(s.Language)
	e := m.baseEmbed(srv, s, fmt.Sprintf(t.recoveredTitle, srv.Name), colorGreen)
	down := inc.runningSince.Sub(inc.start)
	if down < 0 {
		down = now.Sub(inc.start)
	}
	e.Description = fmt.Sprintf(t.recoveredBody, humanDuration(down))
	msg := Message{Embeds: []Embed{e}}
	msg.withMention(Mention{})
	return msg
}

func (m *Monitor) restartedMessage(srv ServerInfo, s Settings, inc *incident) Message {
	t := texts(s.Language)
	e := m.baseEmbed(srv, s, fmt.Sprintf(t.restartedTitle, srv.Name), colorYellow)
	e.Description = fmt.Sprintf(t.restartedBody, inc.last.Unix())
	e.Fields = exitField(t, inc)
	msg := Message{Embeds: []Embed{e}}
	msg.withMention(Mention{})
	return msg
}

func (m *Monitor) errorMessage(srv ServerInfo, s Settings, ev shim.Event) Message {
	t := texts(s.Language)
	e := m.baseEmbed(srv, s, fmt.Sprintf(t.errorTitle, srv.Name), colorOrange)
	lines := ev.Lines
	if len(lines) == 0 && ev.Message != "" {
		lines = []string{ev.Message}
	}
	if len(lines) > 20 {
		lines = lines[:20]
	}
	if s.IncludeLogs && len(lines) > 0 {
		e.Description = codeBlock(lines)
	}
	msg := Message{Embeds: []Embed{e}}
	msg.withMention(Mention{})
	return msg
}

func humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h, mnt, sec := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	units := [3]string{"h", "min", "s"}
	var parts []string
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", h, units[0]))
	}
	if mnt > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", mnt, units[1]))
	}
	if sec > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d %s", sec, units[2]))
	}
	return strings.Join(parts, " ")
}

func escapeMarkdown(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `_`, `\_`, "`", "\\`", `~`, `\~`, `|`, `\|`, `>`, `\>`, `@`, "@​")
	return r.Replace(s)
}

const (
	KindExpired  = "expired"
	KindExpiring = "expiring"
)

func (m *Monitor) Notify(ctx context.Context, srv ServerInfo, kind string, when time.Time) {
	m.start()
	st, err := m.Store.Settings(ctx)
	if err != nil || st.Webhook == "" {
		return
	}
	pl := st.Language != "en"
	var title, desc string
	color := colorOrange
	switch kind {
	case KindExpired:
		if pl {
			title, desc = srv.Name+" wygasł", fmt.Sprintf("Termin minął <t:%d:f>. Serwer został zatrzymany i zablokowany, ale nic nie zostało usunięte. Zdecyduj w panelu: przedłuż albo usuń.", when.Unix())
		} else {
			title, desc = srv.Name+" has expired", fmt.Sprintf("Its end date passed <t:%d:f>. The server was stopped and locked; nothing was deleted. Decide in the panel: extend or delete.", when.Unix())
		}
	case KindExpiring:
		color = colorYellow
		if pl {
			title, desc = srv.Name+" wkrótce wygaśnie", fmt.Sprintf("Wygasa <t:%d:R> (<t:%d:f>). Przedłuż w panelu, jeśli ma dalej działać.", when.Unix(), when.Unix())
		} else {
			title, desc = srv.Name+" expires soon", fmt.Sprintf("It expires <t:%d:R> (<t:%d:f>). Extend it in the panel if it should keep running.", when.Unix(), when.Unix())
		}
	default:
		return
	}
	e := m.baseEmbed(srv, st, title, color)
	e.Description = desc
	msg := Message{Embeds: []Embed{e}}
	msg.withMention(Mention{})
	m.enqueue(st.Webhook, msg, srv, kind)
}
