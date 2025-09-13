package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-message/mail"
)

type CountResult struct {
	Days     map[string]int  `json:"days"`
	Emails   map[string]int  `json:"emails"`
	DaysMap  map[string]*Day `json:"daysMap"`
	Warnings []string        `json:"warnings,omitempty"`
}

func countDays(server, user, pass, mailbox string, location *time.Location, countries ...string) (res CountResult, err error) {
	res.Days = make(map[string]int)
	res.Emails = make(map[string]int)
	res.DaysMap = make(map[string]*Day)

	dayMap := make(map[string]map[string]int)
	for _, country := range countries {
		res.Days[country] = 0
		res.Emails[country] = 0
		dayMap[country] = make(map[string]int)
	}

	c, closeImap, err := connectIMAP(server, user, pass, mailbox)
	if err != nil {
		return res, fmt.Errorf("connecting to IMAP: %w", err)
	}
	defer closeImap()

	// LIST con channels
	mailboxes := make(chan *imap.MailboxInfo, 50) // buffer opcional
	listDone := make(chan error, 1)

	go func() {
		// root "", patrón "*" para todo
		listDone <- c.List("", "*", mailboxes)
	}()

	var trashName string

	for mb := range mailboxes {
		slog.Debugf("Mailbox: %s  Attrs: %v", mb.Name, mb.Attributes)

		for _, a := range mb.Attributes {
			// algunos servidores no exponen constantes; compara con la cadena literal
			if strings.EqualFold(a, `\Trash`) {
				trashName = mb.Name
			}
		}
	}

	// Error final del LIST
	if err := <-listDone; err != nil {
		return res, fmt.Errorf("listing mailboxes: %w", err)
	}

	if trashName == "" {
		trashName = "[Gmail]/Papelera" // valor por defecto si no se encuentra
	}

	slog.Info("Trash mailbox:", trashName)

	// --- Search and fetch emails ---
	seqNums, err := c.Search(imap.NewSearchCriteria())
	if err != nil {
		return res, fmt.Errorf("searching emails: %w", err)
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(seqNums...)

	section := &imap.BodySectionName{}
	messages := make(chan *imap.Message, 100)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope, imap.FetchBody, imap.FetchFlags, imap.FetchUid, section.FetchItem()}, messages)
	}()

	for msg := range messages {
		if msg == nil {
			continue
		}
		r := msg.GetBody(section)
		if r == nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("No body for message %d", msg.SeqNum))
			slog.Warnf("FETCH command did not return body section for email: %v", msg.SeqNum)
			continue
		}

		mr, err := mail.CreateReader(r)
		if err != nil {
			return res, fmt.Errorf("creating mail reader: %w", err)
		}

		date, err := mr.Header.Date()
		if err != nil {
			return res, fmt.Errorf("parsing date header: %w", err)
		}

		emailTime := date.In(location)
		emailDate := emailTime.Format(time.DateOnly)
		emailFullDate := emailTime.Format("02 Jan 2006 15:04:05 MST")

		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return res, fmt.Errorf("getting next part for email %s: %w", emailDate, err)
			}

			switch p.Header.(type) {
			case *mail.InlineHeader:
				b, _ := io.ReadAll(p.Body)
				body := strings.ReplaceAll(strings.TrimSpace(string(b)), "\r\n", " | ")
				if strings.Contains(body, "<html>") {
					continue
				}
				for _, country := range countries {
					if strings.Contains(body, country) {
						res.Emails[country]++
						dayMap[country][emailDate]++
						if dayMap[country][emailDate] == 1 { // first email for this day
							res.Days[country]++

							if res.DaysMap[emailDate] == nil {
								res.DaysMap[emailDate] = &Day{
									Day:     emailDate,
									Andorra: boolToInt(strings.ToLower(country) == "andorra"),
									Spain:   boolToInt(strings.ToLower(country) == "spain"),
								}
							} else {
								switch strings.ToLower(country) {
								case "andorra":
									d := res.DaysMap[emailDate]
									d.Andorra = 1
									res.DaysMap[emailDate] = d
								case "spain":
									d := res.DaysMap[emailDate]
									d.Spain = 1
									res.DaysMap[emailDate] = d
								}
							}
							slog.Debugf("Emails %s body: %s", emailFullDate, body)
							slog.Debugf("%s day: %s [%d]", country, emailDate, res.Days[country])
						}

						if dayMap[country][emailDate] > 5 {
							// Delete this email
							delSet := new(imap.SeqSet)
							delSet.AddNum(msg.SeqNum)

							err = c.Move(delSet, trashName)
							if err != nil {
								slog.Errorf("Moving %s to %s: %w", emailDate, emailFullDate, err)
							}
							slog.Debugf("Moved email %s (%d) to Trash body: %s", emailFullDate, msg.SeqNum, body)
						}
					}
				}
			}
		}
	}

	if err := <-done; err != nil {
		return res, fmt.Errorf("fetching emails: %w", err)
	}

	return res, nil
}

func consoleCount(server, user, pass, mailbox string, location *time.Location, countries ...string) {
	res, err := countDays(server, user, pass, mailbox, location, countries...)
	if err != nil {
		slog.Errorf("initial countDays failed: %v", err)
		return
	}
	for country, c := range res.Emails {
		slog.Debugf("Total Emails Count: %s: %d", country, c)
	}
	for country, c := range res.Days {
		slog.Infof("Final Day Count: %s: %d", country, c)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
