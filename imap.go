package main

import (
	"fmt"

	"github.com/emersion/go-imap/client"
)

func connectIMAP(server, user, pass, mailbox string) (*client.Client, func(), error) {
	c, err := client.DialTLS(server, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("dialing IMAP server %s: %w", server, err)
	}

	slog.Debug("connected to server")

	err = c.Login(user, pass)
	if err != nil {
		_ = c.Close()
		return nil, nil, fmt.Errorf("logging in: %w", err)
	}
	slog.Debug("logged in successfully")
	//defer c.Logout()

	_, err = c.Select(mailbox, false)
	if err != nil {
		_ = c.Logout()
		_ = c.Close()
		return nil, nil, fmt.Errorf("selecting mailbox %s: %w", mailbox, err)
	}

	return c, func() {
		_ = c.Logout()
		_ = c.Close()
	}, nil
}
