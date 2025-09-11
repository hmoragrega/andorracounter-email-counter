package main

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var slog *zap.SugaredLogger

func setupLog(logLevel string) {
	level := zap.InfoLevel
	if strings.ToLower(logLevel) == "debug" {
		level = zap.DebugLevel
	}
	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "T",
		LevelKey:       "L",
		NameKey:        "N",
		CallerKey:      "C",
		MessageKey:     "M",
		StacktraceKey:  "S",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}
	consoleEncoder := zapcore.NewConsoleEncoder(encoderCfg)
	core := zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), level)
	slog = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)).Sugar()
}

func main() {
	// --- Load environment ---
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("loading .env file: %v", err)
	}
	setupLog(os.Getenv("LOG_LEVEL"))
	defer slog.Sync()

	timezone, err := time.LoadLocation(os.Getenv("TIMEZONE"))
	exitOnError(err, "loading timezone")

	imapServer := os.Getenv("GMAIL_IMAP")
	imapUsername := os.Getenv("GMAIL_ACCOUNT")
	imapPassword := os.Getenv("GMAIL_APP_PASS")
	imapMailboxName := os.Getenv("GMAIL_MAILBOX")
	if imapServer == "" {
		imapServer = "imap.gmail.com:993"
	}
	if imapMailboxName == "" {
		imapMailboxName = "MyLocation"
	}
	if imapUsername == "" || imapPassword == "" {
		slog.Fatalf("Please set GMAIL_ACCOUNT and GMAIL_APP_PASS environment variables.")
	}

	res, err := countDays(imapServer, imapUsername, imapPassword, imapMailboxName, timezone, "Andorra", "Spain")
	exitOnError(err, "counting emails")

	for country, c := range res.Email {
		slog.Debugf("Total Email Count: %s: %d", country, c)
	}
	for country, c := range res.Days {
		slog.Infof("Final Day Count: %s: %d", country, c)
	}
}

func exitOnError(err error, context string) {
	if err != nil {
		slog.Fatalf("%s: %v", context, err)
	}
}
