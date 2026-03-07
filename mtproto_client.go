package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

type MTProtoClient struct {
	client *telegram.Client
	ctx    context.Context
}

func NewMTProtoClient(apiID int, apiHash string) (*MTProtoClient, error) {
	options := telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{
			Path: "mtproto_session.json",
		},
		APIID:   apiID,
		APIHash: apiHash,
	}

	client := telegram.NewClient(options)
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
		uploadedFile, err := m.client.Upload().Upload(ctx, &tg.UploadFile{
			File:     tg.NewInputFileBytes(fileName, fileContent),
			FileType: &tg.InputDocument{},
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
		_, err = m.client.Messages().SendDocument(ctx, &tg.MessagesSendDocumentRequest{
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
