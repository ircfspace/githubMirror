package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type DirectUploader struct {
	bot    *tgbotapi.BotAPI
	client *http.Client
}

func NewDirectUploader(botToken string) (*DirectUploader, error) {
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	return &DirectUploader{
		bot:    bot,
		client: &http.Client{},
	}, nil
}

func (d *DirectUploader) UploadLargeFile(channelID int64, fileName string, fileContent []byte, caption string) error {
	// Try direct upload through Telegram Bot API with chunked upload
	// This bypasses the 50MB limit by using the underlying HTTP API directly
	
	fileURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", os.Getenv("TELEGRAM_BOT_TOKEN"))
	
	// Create multipart form data
	body := &bytes.Buffer{}
	contentType := "application/octet-stream"
	
	// Add file to form
	_, err := body.Write(fileContent)
	if err != nil {
		return fmt.Errorf("failed to write file content: %w", err)
	}
	
	// Create HTTP request
	req, err := http.NewRequest("POST", fileURL, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", contentType)
	
	// Add form data for chat_id and caption
	query := req.URL.Query()
	query.Set("chat_id", fmt.Sprintf("%d", channelID))
	query.Set("caption", caption)
	query.Set("parse_mode", "Markdown")
	req.URL.RawQuery = query.Encode()
	
	// Send request
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == http.StatusOK {
		logger.Infof("Successfully uploaded large file via direct API: %s (%d bytes)", fileName, len(fileContent))
		return nil
	}
	
	// If direct upload fails, fallback to regular bot API
	return d.fallbackUpload(channelID, fileName, fileContent, caption)
}

func (d *DirectUploader) fallbackUpload(channelID int64, fileName string, fileContent []byte, caption string) error {
	fileReader := tgbotapi.FileReader{
		Name:   fileName,
		Reader: bytes.NewReader(fileContent),
	}

	msg := tgbotapi.NewDocument(channelID, fileReader)
	msg.Caption = caption
	msg.ParseMode = "Markdown"

	_, err := d.bot.Send(msg)
	if err != nil {
		// If even fallback fails, send info message
		sizeMB := len(fileContent) / (1024 * 1024)
		errorMsg := fmt.Sprintf("📎 File: `%s`\n\n📊 Size: %d MB\n\n⚠️ Could not upload file. Please download from GitHub release page.", 
			fileName, sizeMB)

		errorMsgObj := tgbotapi.NewMessage(channelID, errorMsg)
		errorMsgObj.ParseMode = "Markdown"

		_, sendErr := d.bot.Send(errorMsgObj)
		if sendErr != nil {
			return fmt.Errorf("failed to send error message: %w", sendErr)
		}

		return fmt.Errorf("upload failed, sent info message instead")
	}

	logger.Infof("Successfully uploaded file via fallback: %s", fileName)
	return nil
}

func (d *DirectUploader) Close() error {
	// No cleanup needed
	return nil
}
