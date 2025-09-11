package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/mail"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var slog *zap.SugaredLogger

func setupLog(logLevel string) {
	level := zap.InfoLevel
	switch strings.ToLower(logLevel) {
	case "debug":
		level = zap.DebugLevel
	}
	encoderCfg := zapcore.EncoderConfig{
		TimeKey:       "T",
		LevelKey:      "L",
		NameKey:       "N",
		CallerKey:     "C",
		MessageKey:    "M",
		StacktraceKey: "S",
		LineEnding:    zapcore.DefaultLineEnding,
		EncodeLevel:   zapcore.CapitalColorLevelEncoder, // 🎨 Colors!
		//EncodeTime:     zapcore.TimeEncoderOfLayout("15:04:05"),
		EncodeDuration: zapcore.StringDurationEncoder,
		//EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	consoleEncoder := zapcore.NewConsoleEncoder(encoderCfg)
	core := zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), level)
	slog = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)).Sugar()
}

func main() {
	var (
		AndorraDays = 0
		SpainDays   = 0

		AndorraTotal = 0
		SpainTotal   = 0

		AndorraMap = make(map[string]bool)
		SpainMap   = make(map[string]bool)
	)

	err := godotenv.Load()
	if err != nil {
		log.Fatalf("loading .env file: %v", err)
	}
	setupLog(os.Getenv("LOG_LEVEL"))

	timezone, err := time.LoadLocation(os.Getenv("TIMEZONE"))
	exitOnError(err, "loading timezone")

	// --- Configuration ---
	// IMPORTANT: It's highly recommended to use environment variables or a secure
	// configuration method instead of hardcoding credentials in the source code.
	// For Gmail, you will need to generate an "App Password".
	// See: https://support.google.com/accounts/answer/185833
	var (
		imapServer      = os.Getenv("GMAIL_IMAP")
		imapUsername    = os.Getenv("GMAIL_ACCOUNT")
		imapPassword    = os.Getenv("GMAIL_APP_PASS")
		imapMailboxName = os.Getenv("GMAIL_MAILBOX")
	)
	if imapServer == "" {
		imapServer = "imap.gmail.com:993"
	}
	if imapMailboxName == "" {
		imapMailboxName = "MyLocation"
	}
	if imapUsername == "" || imapPassword == "" {
		slog.Fatalf("Please set GMAIL_ACCOUNT and GMAIL_APP_PASS environment variables.")
	}

	// --- Connect to Server ---
	// The client will use TLS by default with a ":993" port.
	c, err := imapclient.DialTLS(imapServer, &imapclient.Options{Dialer: nil})
	exitOnError(err, "dialing server")

	defer c.Close()
	slog.Debug("connected to server")

	err = c.Login(imapUsername, imapPassword).Wait()
	exitOnError(err, "logging in")
	slog.Debug("logged in successfully")
	defer c.Logout()

	_, err = c.Select(imapMailboxName, nil).Wait()
	exitOnError(err, fmt.Sprintf("selecting %s mailbox", imapMailboxName))

	// search all emails
	searchResult, err := c.Search(&imap.SearchCriteria{}, nil).Wait()
	exitOnError(err, "searching emails")

	// fetch the body of the emails
	fetchOptions := &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{{
			Peek: true,
		}},
	}
	fetchCmd := c.Fetch(searchResult.All, fetchOptions)

	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break // No more messages
		}
		//slog.Debugf("Reading Message ID %d", msg.SeqNum)

		// Find the body section in the response
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
			slog.Warnf("FETCH command did not return body section")
			continue
		}

		// Read the message via the go-message library
		mr, err := mail.CreateReader(bodySectionData.Literal)
		if err != nil {
			exitOnError(err, "creating mail reader")
		}

		date, err := mr.Header.Date()
		exitOnError(err, "getting date for email")

		emailTime := date.In(timezone)
		emailDate := emailTime.Format("02 Jan 2006")
		emailFullDate := emailTime.Format("02 Jan 2006 15:04:05 MST")

		// Process the message's parts
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			exitOnError(err, fmt.Sprintf("getting part for email '%s'", emailDate))

			switch p.Header.(type) {
			case *mail.InlineHeader:
				// This is the message's text (can be plain-text or HTML)
				b, _ := io.ReadAll(p.Body)
				body := strings.TrimSpace(string(b))

				if strings.Contains(body, "<html>") {
					continue
				}

				if strings.Contains(body, "Andorra") {
					AndorraTotal++
					if !AndorraMap[emailDate] {
						AndorraMap[emailDate] = true
						AndorraDays++
						slog.Debugf("Email %s body: %v", emailFullDate, body)
						slog.Debugf("Andorra day: %s [%d]", emailDate, AndorraDays)
					}
				}

				if strings.Contains(string(b), "Spain") {
					SpainTotal++
					if !SpainMap[emailDate] {
						SpainMap[emailDate] = true
						SpainDays++
						slog.Debugf("Email %s body: %v", emailFullDate, body)
						slog.Debugf("Spain day: %s [%d]", emailDate, SpainDays)
						continue
					}
				}
			}
		}
	}

	exitOnError(fetchCmd.Close(), "closing fetch command")

	slog.Debugf("Total Email Count: Andorra: %d - Spain %d", AndorraTotal, SpainTotal)

	slog.Infof("Final Day Count: Andorra: %d - Spain %d", AndorraDays, SpainDays)
	if SpainDays > 160 {
		slog.Warnf("Spain days exceed 160 days! Days: %d", SpainDays)
	}
}

func exitOnError(err error, context string) {
	if err != nil {
		slog.Fatalf("%s: %v", context, err)
	}
}
