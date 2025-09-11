package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/mail"
)

type CountResult struct {
	Days     map[string]int `json:"days"`
	Emails   map[string]int `json:"emails"`
	Warnings []string       `json:"warnings,omitempty"`
}

func countDays(server, user, pass, mailbox string, location *time.Location, countries ...string) (res CountResult, err error) {
	res.Days = make(map[string]int)
	res.Emails = make(map[string]int)

	dayMap := make(map[string]map[string]bool)
	for _, country := range countries {
		res.Days[country] = 0
		res.Emails[country] = 0
		dayMap[country] = make(map[string]bool)
	}

	c, closeImap, err := connectIMAP(server, user, pass, mailbox)
	if err != nil {
		return res, fmt.Errorf("connecting to IMAP: %w", err)
	}
	defer closeImap()

	// --- Search and fetch emails ---
	searchResult, err := c.Search(&imap.SearchCriteria{}, nil).Wait()
	if err != nil {
		return res, fmt.Errorf("searching emails: %w", err)
	}

	fetchOptions := &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{{
			Peek: true,
		}},
	}
	fetchCmd := c.Fetch(searchResult.All, fetchOptions)

	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}
		var bodySectionData imapclient.FetchItemDataBodySection
		ok := false
		for {
			item := msg.Next()
			if item == nil {
				break
			}
			bodySectionData, ok = item.(imapclient.FetchItemDataBodySection)
			if ok {
				break
			}
		}
		if !ok {
			res.Warnings = append(res.Warnings, fmt.Sprintf("FETCH command did not return body section for email: %v", msg.SeqNum))
			slog.Warnf("FETCH command did not return body section for email: %v", msg.SeqNum)
			continue
		}

		mr, err := mail.CreateReader(bodySectionData.Literal)
		if err != nil {
			return res, fmt.Errorf("creating mail reader: %w", err)
		}

		date, err := mr.Header.Date()
		if err != nil {
			return res, fmt.Errorf("parsing date header: %w", err)
		}

		emailTime := date.In(location)
		emailDate := emailTime.Format("02 Jan 2006")
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
						if !dayMap[country][emailDate] {
							dayMap[country][emailDate] = true
							res.Days[country]++
							slog.Debugf("Emails %s body: %s", emailFullDate, body)
							slog.Debugf("%s day: %s [%d]", country, emailDate, res.Days[country])
						}
					}
				}
			}
		}
	}

	if err := fetchCmd.Close(); err != nil {
		return res, fmt.Errorf("closing fetch command: %w", err)
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
