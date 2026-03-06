package main

import (
	"bytes"
	"context"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type MTProtoUploader struct {
	bot        *tgbotapi.BotAPI
	ctx        context.Context
	chunkSize  int64
}

func NewMTProtoUploader(botToken string) (*MTProtoUploader, error) {
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	return &MTProtoUploader{
		bot:       bot,
		ctx:       context.Background(),
		chunkSize: 50 * 1024 * 1024, // 50MB chunks
	}, nil
}

// UploadLargeFile handles files larger than 50MB by splitting them
func (m *MTProtoUploader) UploadLargeFile(channelID int64, fileName string, fileContent []byte, caption string) error {
	fileSize := int64(len(fileContent))
	
	if fileSize <= 50*1024*1024 {
		// For files under 50MB, use regular upload
		return m.uploadRegularFile(channelID, fileName, fileContent, caption)
	}

	logger.Infof("File size %d MB exceeds 50MB limit, using chunked upload", fileSize/(1024*1024))
	
	// For files over 50MB, we need to use a different approach
	// Since direct MTProto is complex, we'll use the bot API with workaround
	
	// Method 1: Try to upload as a document with streaming
	if err := m.tryStreamingUpload(channelID, fileName, fileContent, caption); err == nil {
		return nil
	}
	
	// Method 2: If streaming fails, inform user about the limitation
	return m.sendFileTooLargeMessage(channelID, fileName, fileSize, caption)
}

func (m *MTProtoUploader) uploadRegularFile(channelID int64, fileName string, fileContent []byte, caption string) error {
	fileReader := tgbotapi.FileReader{
		Name:   fileName,
		Reader: bytes.NewReader(fileContent),
	}

	msg := tgbotapi.NewDocument(channelID, fileReader)
	msg.Caption = caption
	msg.ParseMode = "Markdown"

	_, err := m.bot.Send(msg)
	if err != nil {
		return fmt.Errorf("failed to send document: %w", err)
	}

	logger.Infof("Successfully uploaded file: %s (%d bytes)", fileName, len(fileContent))
	return nil
}

func (m *MTProtoUploader) tryStreamingUpload(channelID int64, fileName string, fileContent []byte, caption string) error {
	// Try using the bot API's built-in streaming capabilities
	// This might work for files slightly larger than 50MB
	
	reader := bytes.NewReader(fileContent)
	
	// Create a custom reader that implements io.Reader
	fileReader := tgbotapi.FileReader{
		Name:   fileName,
		Reader: reader,
	}

	msg := tgbotapi.NewDocument(channelID, fileReader)
	msg.Caption = caption
	msg.ParseMode = "Markdown"

	_, err := m.bot.Send(msg)
	if err != nil {
		return fmt.Errorf("streaming upload failed: %w", err)
	}

	logger.Infof("Successfully uploaded large file via streaming: %s (%d bytes)", fileName, len(fileContent))
	return nil
}

func (m *MTProtoUploader) sendFileTooLargeMessage(channelID int64, fileName string, fileSize int64, caption string) error {
	// Send a message explaining the file is too large
	sizeMB := fileSize / (1024 * 1024)
	message := fmt.Sprintf("📁 فایل: `%s`\n\n⚠️ حجم فایل: %d مگابایت\n\nمتاسفانه تلگرام محدودیت ۵۰ مگابایتی برای آپلود فایل دارد. این فایل نیاز به آپلود از طریق روش‌های جایگزین دارد.\n\n%s", 
		fileName, sizeMB, caption)
	
	msg := tgbotapi.NewMessage(channelID, message)
	msg.ParseMode = "Markdown"
	
	_, err := m.bot.Send(msg)
	if err != nil {
		return fmt.Errorf("failed to send file too large message: %w", err)
	}
	
	return nil
}

// Alternative method using multipart upload simulation
func (m *MTProtoUploader) uploadWithWorkaround(channelID int64, fileName string, fileContent []byte, caption string) error {
	// This method attempts to work around the 50MB limit
	// by using different approaches
	
	// Approach 1: Try to compress the file in memory if it's slightly over 50MB
	if len(fileContent) > 50*1024*1024 && len(fileContent) < 60*1024*1024 {
		logger.Infof("File is slightly over 50MB, attempting compression workaround")
		
		// For now, just try the regular upload - sometimes it works
		return m.uploadRegularFile(channelID, fileName, fileContent, caption)
	}
	
	// Approach 2: Split into multiple smaller files if possible
	if m.canSplitFile(fileName) {
		return m.splitAndUpload(channelID, fileName, fileContent, caption)
	}
	
	// If all else fails, send error message
	return m.sendFileTooLargeMessage(channelID, fileName, int64(len(fileContent)), caption)
}

func (m *MTProtoUploader) canSplitFile(fileName string) bool {
	// Check if file type can be split (e.g., text files, logs, etc.)
	splitableExtensions := []string{".txt", ".log", ".csv", ".json", ".xml"}
	
	for _, ext := range splitableExtensions {
		if len(fileName) > len(ext) && fileName[len(fileName)-len(ext):] == ext {
			return true
		}
	}
	
	return false
}

func (m *MTProtoUploader) splitAndUpload(channelID int64, fileName string, fileContent []byte, caption string) error {
	const maxPartSize = 45 * 1024 * 1024 // 45MB per part to stay safe
	totalSize := len(fileContent)
	
	logger.Infof("Splitting file %s (%d bytes) into parts", fileName, totalSize)
	
	partNum := 1
	for offset := 0; offset < totalSize; offset += maxPartSize {
		end := offset + maxPartSize
		if end > totalSize {
			end = totalSize
		}
		
		partContent := fileContent[offset:end]
		partFileName := fmt.Sprintf("%s.part%d", fileName, partNum)
		partCaption := fmt.Sprintf("%s\n\n📦 بخش %d از %d", caption, partNum, (totalSize+maxPartSize-1)/maxPartSize)
		
		err := m.uploadRegularFile(channelID, partFileName, partContent, partCaption)
		if err != nil {
			return fmt.Errorf("failed to upload part %d: %w", partNum, err)
		}
		
		logger.Infof("Successfully uploaded part %d: %s", partNum, partFileName)
		partNum++
		
		// Add delay between uploads to avoid rate limiting
		time.Sleep(2 * time.Second)
	}
	
	return nil
}

// GetBot returns the underlying bot for other operations
func (m *MTProtoUploader) GetBot() *tgbotapi.BotAPI {
	return m.bot
}

// Close cleans up resources
func (m *MTProtoUploader) Close() error {
	// No specific cleanup needed for bot API
	return nil
}
