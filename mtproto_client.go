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
		upload := telegram.NewUpload(m.client, telegram.UploadOptions{})
		
		uploadedFile, err := upload.UploadFile(ctx, &tg.InputFile{
			Name: fileName,
			Data: fileContent,
		})
		if err != nil {
			return fmt.Errorf("failed to upload file: %w", err)
		}

		logger.Infof("File uploaded successfully via MTProto: %s (%d bytes)", fileName, len(fileContent))

		// Get channel peer
		peer := &tg.InputPeerChannel{
			ChannelID: channelID,
		}

		// Send document to channel
		req := &tg.MessagesSendMediaRequest{
			Peer: peer,
			Media: &tg.InputMediaDocument{
				ID: uploadedFile,
			},
			Message: caption,
		}

		_, err = m.client.Send(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to send document: %w", err)
		}

		logger.Infof("Document sent successfully to channel: %s", fileName)
		return nil
	})
}

func (m *MTProtoClient) Close() error {
	return m.client.Close()
}
