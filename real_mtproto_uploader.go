package main

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

type RealMTProtoUploader struct {
	client *telegram.Client
	ctx    context.Context
}

func NewRealMTProtoUploader(botToken string) (*RealMTProtoUploader, error) {
	// Create client with bot token
	client := telegram.NewClient(telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{
			Path: "mtproto_session.json",
		},
	})

	ctx := context.Background()

	return &RealMTProtoUploader{
		client: client,
		ctx:    ctx,
	}, nil
}

func (m *RealMTProtoUploader) Connect(botToken string) error {
	return m.client.Run(m.ctx, func(ctx context.Context) error {
		// Authenticate with bot token
		auth := telegram.NewBotAuth(botToken)
		if err := auth.Auth(ctx); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}
		return nil
	})
}

func (m *RealMTProtoUploader) UploadLargeFile(channelID int64, fileName string, fileContent []byte, caption string) error {
	return m.client.Run(m.ctx, func(ctx context.Context) error {
		// Upload file using MTProto - no 50MB limit
		uploadedFile, err := m.client.UploadFile(ctx, &tg.UploadFile{
			File:     tg.NewInputFileBytes(fileName, fileContent),
			FileType: &tg.InputDocument{},
		})
		if err != nil {
			return fmt.Errorf("failed to upload file: %w", err)
		}

		// Send document to channel
		_, err = m.client.SendMessage(ctx, &tg.MessagesSendMessageRequest{
			Peer: &tg.InputPeerChannel{
				ChannelID: channelID,
			},
			Message: caption,
			Media: &tg.InputMediaDocument{
				ID: uploadedFile.ID,
			},
		})

		if err != nil {
			return fmt.Errorf("failed to send document: %w", err)
		}

		return nil
	})
}

func (m *RealMTProtoUploader) Close() error {
	if m.client != nil {
		return m.client.Close()
	}
	return nil
}
