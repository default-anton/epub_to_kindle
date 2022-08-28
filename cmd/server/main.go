package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"

	"go.uber.org/zap"
	"gopkg.in/gomail.v2"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()
	log := logger.Sugar()
	log.Info("Starting...")

	bot, err := tgbotapi.NewBotAPI(os.Getenv("TELEGRAM_API_TOKEN"))
	if err != nil {
		panic(err)
	}

	log.Infof("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil { // ignore any non-Message updates
			continue
		}

		if update.Message.Document == nil {
			continue
		}

		processUpdate(log, bot, &update)
	}
}
func processUpdate(log *zap.SugaredLogger, bot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	fileURL, err := bot.GetFileDirectURL(update.Message.Document.FileID)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Failed to download the file from telegram: %s", err))
		if _, err = bot.Send(msg); err != nil {
			log.Errorf("Failed to send a message %s", err)
		}
		return
	}

	// Get the data
	resp, err := http.Get(fileURL)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Failed to download the file from telegram: %s", err))
		if _, err = bot.Send(msg); err != nil {
			log.Errorf("Failed to send a message %s", err)
		}
		return
	}
	defer resp.Body.Close()

	// Create the file
	file, err := os.CreateTemp(os.TempDir(), "*"+path.Ext(update.Message.Document.FileName))
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Failed to download the file from telegram: %s", err))
		if _, err = bot.Send(msg); err != nil {
			log.Errorf("Failed to send a message %s", err)
		}
		return
	}
	defer func() {
		filePath := file.Name()
		if err = file.Close(); err != nil {
			log.Errorf("Failed to close the file: %s. Error: %s", filePath, err)
		}
		if err = os.Remove(filePath); err != nil {
			log.Errorf("Failed to delete the file: %s. Error: %s", filePath, err)
		}
	}()

	// Write the body to file
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Failed to download the file from telegram: %s", err))
		if _, err = bot.Send(msg); err != nil {
			log.Errorf("Failed to send a message %s", err)
		}
		return
	}

	extension := path.Ext(file.Name())
	epubPath := replaceExt(file.Name(), ".epub")
	if extension != ".epub" {
		cmd := exec.Command("ebook-convert", file.Name(), epubPath)
		err = cmd.Run()
		if err != nil {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Failed to convert the book to epub: %s", err))
			if _, err = bot.Send(msg); err != nil {
				log.Errorf("Failed to send a message %s", err)
			}
			return
		}
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Sending the book to your kindle...")
	if _, err = bot.Send(msg); err != nil {
		log.Errorf("Failed to send a message %s", err)
		return
	}

	from := os.Getenv("SMTP_EMAIL")
	password := os.Getenv("SMTP_PASSWORD")
	to := os.Getenv("KINDLE_EMAIL")
	// smtp server configuration.
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort, err := strconv.Atoi(os.Getenv("SMTP_PORT"))
	if err != nil {
		msg = tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Failed to parse smpt port: %s", err))
		if _, err = bot.Send(msg); err != nil {
			log.Errorf("Failed to send a message %s", err)
		}
		return
	}

	email := gomail.NewMessage()
	email.SetHeader("From", from)
	email.SetHeader("To", to)

	email.Attach(epubPath, gomail.Rename(replaceExt(update.Message.Document.FileName, ".epub")))

	d := gomail.NewDialer(smtpHost, smtpPort, from, password)

	if err = d.DialAndSend(email); err != nil {
		msg = tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Failed to send the letter: %s", err))
		if _, err = bot.Send(msg); err != nil {
			log.Errorf("Failed to send a message %s", err)
		}
		return
	}

	log.Info("Email sent successfully")

	msg = tgbotapi.NewMessage(update.Message.Chat.ID, "Email sent successfully!")
	if _, err = bot.Send(msg); err != nil {
		log.Errorf("Failed to send a message %s", err)
		return
	}
}

func replaceExt(filepath, ext string) string {
	return filepath[:len(filepath)-len(path.Ext(filepath))] + ext
}
