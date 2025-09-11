package main

import (
	"fmt"

	"github.com/emersion/go-imap/v2/imapclient"
)

func connectIMAP(server, user, pass, mailbox string) (*imapclient.Client, func(), error) {
	c, err := imapclient.DialTLS(server, &imapclient.Options{Dialer: nil})
	if err != nil {
		return nil, nil, fmt.Errorf("dialing IMAP server %s: %w", server, err)
	}

	slog.Debug("connected to server")

	err = c.Login(user, pass).Wait()
	if err != nil {
		_ = c.Close()
		return nil, nil, fmt.Errorf("logging in: %w", err)
	}
	slog.Debug("logged in successfully")
	//defer c.Logout()

	_, err = c.Select(mailbox, nil).Wait()
	if err != nil {
		c.Logout()
		_ = c.Close()
		return nil, nil, fmt.Errorf("selecting mailbox %s: %w", mailbox, err)
	}

	return c, func() {
		c.Logout()
		_ = c.Close()
	}, nil
}
