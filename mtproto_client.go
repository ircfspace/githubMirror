package main

import (
	"context"
	"fmt"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

type MTProtoClient struct {
	client *telegram.Client
	ctx    context.Context
}

func NewMTProtoClient(apiID int, apiHash string) (*MTProtoClient, error) {
	client := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{
			Path: "mtproto_session.json",
		},
	})

	ctx := context.Background()

	return &MTProtoClient{
		client: client,
		ctx:    ctx,
	}, nil
}

func (m *MTProtoClient) Connect() error {
	return m.client.Run(m.ctx, func(ctx context.Context) error {
		logger.Infof("MTProto client connected successfully - NO 50MB LIMIT!")
		return nil
	})
}

func (m *MTProtoClient) UploadFileToChannel(channelID int64, fileName string, fileContent []byte, caption string) error {
	return m.client.Run(m.ctx, func(ctx context.Context) error {
		// Upload file using MTProto - NO 50MB LIMIT!
		upload, err := m.client.Upload().UploadBigFile(ctx, fileName, len(fileContent))
		if err != nil {
			return fmt.Errorf("failed to start upload: %w", err)
		}

		// Upload file parts
		_, err = upload.Write(fileContent)
		if err != nil {
			return fmt.Errorf("failed to upload file content: %w", err)
		}

		uploadedFile, err := upload.Result()
		if err != nil {
			return fmt.Errorf("failed to get upload result: %w", err)
		}

		logger.Infof("File uploaded successfully via MTProto: %s (%d bytes)", fileName, len(fileContent))

		// Get channel peer
		peer := &tg.InputPeerChannel{
			ChannelID: channelID,
		}

		// Send document to channel
		_, err = m.client.API().MessagesSendDocument(ctx, &tg.MessagesSendDocumentRequest{
			Peer:    peer,
			Message: caption,
			File:    uploadedFile,
		})

		if err != nil {
			return fmt.Errorf("failed to send document: %w", err)
		}

		logger.Infof("Document sent successfully to channel: %s", fileName)
		return nil
	})
}

func (m *MTProtoClient) Close() error {
	if m.client != nil {
		return m.client.Close()
	}
	return nil
}
