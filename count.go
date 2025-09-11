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
	Days     map[string]int
	Email    map[string]int
	Warnings []string
}

func countDays(server, user, pass, mailbox string, location *time.Location, countries ...string) (res CountResult, err error) {
	res.Days = make(map[string]int)
	res.Email = make(map[string]int)

	dayMap := make(map[string]map[string]bool)
	for _, country := range countries {
		res.Days[country] = 0
		res.Email[country] = 0
		dayMap[country] = make(map[string]bool)
	}

	// --- Connect to IMAP server ---
	c, err := imapclient.DialTLS(server, &imapclient.Options{Dialer: nil})
	exitOnError(err, "dialing server")
	defer c.Close()
	slog.Debug("connected to server")

	err = c.Login(user, pass).Wait()
	exitOnError(err, "logging in")
	slog.Debug("logged in successfully")
	defer c.Logout()

	_, err = c.Select(mailbox, nil).Wait()
	if err != nil {
		return res, fmt.Errorf("selecting mailbox %s: %w", mailbox, err)
	}

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
			exitOnError(err, "creating mail reader")
		}

		date, err := mr.Header.Date()
		exitOnError(err, "getting date for email")

		emailTime := date.In(location)
		emailDate := emailTime.Format("02 Jan 2006")
		emailFullDate := emailTime.Format("02 Jan 2006 15:04:05 MST")

		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			exitOnError(err, fmt.Sprintf("getting part for email '%s'", emailDate))

			switch p.Header.(type) {
			case *mail.InlineHeader:
				b, _ := io.ReadAll(p.Body)
				body := strings.ReplaceAll(strings.TrimSpace(string(b)), "\r\n", " | ")
				if strings.Contains(body, "<html>") {
					continue
				}
				for _, country := range countries {
					if strings.Contains(body, country) {
						res.Email[country]++
						if !dayMap[country][emailDate] {
							dayMap[country][emailDate] = true
							res.Days[country]++
							slog.Debugf("Email %s body: %s", emailFullDate, body)
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
