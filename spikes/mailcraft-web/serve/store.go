package main

import (
	"bytes"
	"fmt"
	"io"
	"net/mail"
	"sort"
	"strings"
	"sync"
	"time"
)

// store is one account's mail in memory: what a facade over the PIM would answer from the sidecar.
type store struct {
	mu        sync.Mutex
	account   string
	owner     string
	state     int
	mailboxes []mailbox
	emails    map[string]*email
	blobs     map[string][]byte
	next      int
}

type mailbox struct {
	ID, Name, Role string
	Sort           int
}

type email struct {
	ID, Blob   string
	Mailboxes  map[string]bool
	Keywords   map[string]bool
	ReceivedAt time.Time
	Raw        []byte
}

func newStore(account, owner string) *store {
	s := &store{account: account, owner: owner, emails: map[string]*email{}, blobs: map[string][]byte{}}
	for i, m := range []mailbox{{"inbox", "Inbox", "inbox", 1}, {"drafts", "Drafts", "drafts", 2}, {"sent", "Sent", "sent", 3},
		{"archive", "Archive", "archive", 4}, {"trash", "Trash", "trash", 5}, {"junk", "Junk", "junk", 6}} {
		m.Sort = i + 1
		s.mailboxes = append(s.mailboxes, m)
	}
	base := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	for i, m := range seed {
		raw := fmt.Sprintf("From: %s\r\nTo: Ada <%s>\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <seed-%d@aura.test>\r\n"+
			"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
			m.from, owner, m.subject, base.Add(time.Duration(i)*time.Hour).Format(time.RFC1123Z), i, m.body)
		keywords := map[string]bool{}
		if m.seen {
			keywords["$seen"] = true
		}
		s.add([]byte(raw), map[string]bool{"inbox": true}, keywords, base.Add(time.Duration(i)*time.Hour))
	}
	return s
}

var seed = []struct {
	from, subject, body string
	seen                bool
}{
	{"Marco Rossi <marco@example.org>", "Offerta impianto elettrico", "Ciao Ada,\r\nin allegato trovi l'offerta aggiornata.\r\nA presto, Marco", true},
	{"Comune di Pordenone <protocollo@example.org>", "Convocazione riunione 3 dicembre", "Gentile utente,\r\nla riunione e' fissata per il 3 dicembre alle 10.", false},
	{"Giulia Bianchi <giulia@example.org>", "Re: verbale di ieri", "Ti giro le correzioni al verbale.\r\nGiulia", false},
}

// add keeps a message and its blob, and moves the account's state on.
func (s *store) add(raw []byte, mailboxes, keywords map[string]bool, at time.Time) *email {
	s.next++
	e := &email{ID: fmt.Sprintf("M%d", s.next), Blob: fmt.Sprintf("B%d", s.next), Mailboxes: mailboxes, Keywords: keywords, ReceivedAt: at, Raw: raw}
	s.emails[e.ID] = e
	s.blobs[e.Blob] = raw
	s.state++
	return e
}

func (s *store) stateString() string { return fmt.Sprintf("st%d", s.state) }

// inMailbox lists a mailbox's emails newest first, as Email/query's default receivedAt sort asks.
func (s *store) inMailbox(id string) []*email {
	var out []*email
	for _, e := range s.emails {
		if id == "" || e.Mailboxes[id] {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReceivedAt.After(out[j].ReceivedAt) })
	return out
}

// header is a message's header and its plain-text body, read with net/mail.
func header(raw []byte) (mail.Header, string) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return mail.Header{}, ""
	}
	body, _ := io.ReadAll(io.LimitReader(m.Body, 1<<20))
	return m.Header, string(body)
}

func addresses(h mail.Header, key string) any {
	list, err := h.AddressList(key)
	if err != nil {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{"name": a.Name, "email": a.Address})
	}
	return out
}

func preview(body string) string {
	p := strings.Join(strings.Fields(body), " ")
	if len(p) > 256 {
		p = p[:256]
	}
	return p
}
