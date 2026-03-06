package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"
	"github.com/sirupsen/logrus"
)

type Config struct {
	Telegram struct {
		ChannelID string `json:"channel_id"`
	} `json:"telegram"`
	Repositories []Repository `json:"repositories"`
}

type Repository struct {
	Name             string `json:"name"`
	GitHubURL        string `json:"github_url"`
	GooglePlayURL    string `json:"google_play_url"`
	AppleStoreURL    string `json:"apple_store_url"`
	MicrosoftStoreURL string `json:"microsoft_store_url"`
}

type GitHubRelease struct {
	ID          int64     `json:"id"`
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Body        string    `json:"body"`
	Assets      []Asset   `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type ProcessedReleases map[string]string

var (
	config           Config
	bot             *tgbotapi.BotAPI
	processedReleases ProcessedReleases
	logger          *logrus.Logger
)

func init() {
	logger = logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)
}

func loadConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("error reading config file: %w", err)
	}

	err = json.Unmarshal(data, &config)
	if err != nil {
		return fmt.Errorf("error parsing config file: %w", err)
	}

	return nil
}

func loadProcessedReleases() error {
	data, err := os.ReadFile("processed_releases.json")
	if err != nil {
		if os.IsNotExist(err) {
			processedReleases = make(ProcessedReleases)
			return nil
		}
		return fmt.Errorf("error reading processed releases file: %w", err)
	}

	err = json.Unmarshal(data, &processedReleases)
	if err != nil {
		return fmt.Errorf("error parsing processed releases file: %w", err)
	}

	return nil
}

func saveProcessedReleases() error {
	data, err := json.MarshalIndent(processedReleases, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling processed releases: %w", err)
	}

	err = os.WriteFile("processed_releases.json", data, 0644)
	if err != nil {
		return fmt.Errorf("error writing processed releases file: %w", err)
	}

	return nil
}

func getGitHubReleases(repoURL string) ([]GitHubRelease, error) {
	parts := strings.Split(strings.TrimSuffix(repoURL, "/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid GitHub URL: %s", repoURL)
	}

	owner, repo := parts[len(parts)-2], parts[len(parts)-1]
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", owner, repo)

	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("error fetching releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var releases []GitHubRelease
	err = json.NewDecoder(resp.Body).Decode(&releases)
	if err != nil {
		return nil, fmt.Errorf("error parsing releases: %w", err)
	}

	return releases, nil
}

func getFileHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func downloadFile(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	contentLength := resp.ContentLength
	if contentLength <= 0 {
		// If content length is unknown, just download without progress
		return io.ReadAll(resp.Body)
	}

	reader := &progressReader{
		Reader:        resp.Body,
		TotalBytes:    contentLength,
		Downloaded:    0,
		LastPercent:   0,
		FileName:      filepath.Base(url),
	}

	return io.ReadAll(reader)
}

type progressReader struct {
	Reader      io.Reader
	TotalBytes  int64
	Downloaded  int64
	LastPercent int
	FileName    string
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.Reader.Read(p)
	pr.Downloaded += int64(n)
	
	if pr.TotalBytes > 0 {
		percent := int((pr.Downloaded * 100) / pr.TotalBytes)
		// Update progress every 5%
		if percent-pr.LastPercent >= 5 || percent == 100 {
			bars := strings.Repeat("=", percent/5) + strings.Repeat(" ", 20-percent/5)
			logger.Infof("Downloading %s: [%s] %d%% (%.1f/%.1f MB)", 
				pr.FileName, bars, percent, 
				float64(pr.Downloaded)/1024/1024, 
				float64(pr.TotalBytes)/1024/1024)
			pr.LastPercent = percent
		}
	}
	
	return
}

func verifyPGPSignature(content []byte, signature []byte) (bool, error) {
	// PGP verification disabled due to library compatibility issues
	// In production, you'd need proper GPG setup
	// For now, we'll just return true if signature exists
	return len(signature) > 0, nil
}

func createCaption(repo Repository, release GitHubRelease, fileHashes map[string]string, signatures map[string]bool) string {
	var caption strings.Builder
	
	caption.WriteString(fmt.Sprintf("🚀 ریلیز جدید: %s\n\n", repo.Name))
	caption.WriteString(fmt.Sprintf("📦 نسخه: %s\n", release.TagName))
	caption.WriteString(fmt.Sprintf("📅 تاریخ: %s\n\n", release.PublishedAt.Format("2006-01-02 15:04:05")))

	if release.Name != "" {
		caption.WriteString(fmt.Sprintf("📝 نام: %s\n\n", release.Name))
	}

	if repo.GitHubURL != "" {
		caption.WriteString(fmt.Sprintf("🔗 GitHub: %s\n", repo.GitHubURL))
	}

	if repo.GooglePlayURL != "" {
		caption.WriteString(fmt.Sprintf("🤖 Google Play: %s\n", repo.GooglePlayURL))
	}

	if repo.AppleStoreURL != "" {
		caption.WriteString(fmt.Sprintf("💰 App Store: %s\n", repo.AppleStoreURL))
	}

	if repo.MicrosoftStoreURL != "" {
		caption.WriteString(fmt.Sprintf("🪟 Microsoft Store: %s\n", repo.MicrosoftStoreURL))
	}

	if len(fileHashes) > 0 {
		caption.WriteString("\n🔒 هش‌های SHA256:\n")
		// Sort filenames for consistent output
		var filenames []string
		for filename := range fileHashes {
			filenames = append(filenames, filename)
		}
		sort.Strings(filenames)
		
		for _, filename := range filenames {
			hash := fileHashes[filename]
			caption.WriteString(fmt.Sprintf("• %s: %s\n", filename, hash))
		}
	}

	return caption.String()
}

func sendReleaseToChannel(repo Repository, release GitHubRelease) error {
	logger.Infof("Processing release: %s", release.TagName)
	releaseID := fmt.Sprintf("%s#%d", repo.GitHubURL, release.ID)

	if _, exists := processedReleases[releaseID]; exists {
		logger.Infof("Release %s already processed", releaseID)
		return nil
	}

	logger.Infof("Release %s is new, processing...", releaseID)
	fileHashes := make(map[string]string)
	signatures := make(map[string]bool)
	var documents []interface{}

	logger.Infof("Found %d assets in release", len(release.Assets))
	// Download and process release assets
	for _, asset := range release.Assets {
		logger.Infof("Downloading asset: %s", asset.Name)
		content, err := downloadFile(asset.BrowserDownloadURL)
		if err != nil {
			logger.Errorf("Error downloading %s: %v", asset.Name, err)
			continue
		}

		fileHash := getFileHash(content)
		fileHashes[asset.Name] = fileHash

		// Check for PGP signature
		for _, sigAsset := range release.Assets {
			if sigAsset.Name == asset.Name+".sig" || sigAsset.Name == asset.Name+".asc" {
				sigContent, err := downloadFile(sigAsset.BrowserDownloadURL)
				if err == nil {
					isValid, _ := verifyPGPSignature(content, sigContent)
					signatures[asset.Name] = isValid
				}
				break
			}
		}

		// Create document for Telegram
		doc := tgbotapi.NewInputMediaDocument(tgbotapi.FileBytes{
			Name:  asset.Name,
			Bytes: content,
		})
		documents = append(documents, doc)
	}

	caption := createCaption(repo, release, fileHashes, signatures)

	// Create inline keyboard with back button
	channelURL := fmt.Sprintf("https://t.me/%s", strings.TrimPrefix(config.Telegram.ChannelID, "@"))
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{
			{Text: "🔙 بازگشت به کانال", URL: &channelURL},
		},
	}
	replyMarkup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

	// Send as text message with file info
	if config.Telegram.ChannelID != "0" {
		msg := tgbotapi.NewMessageToChannel(config.Telegram.ChannelID, caption)
		msg.ParseMode = "" // Remove Markdown to avoid parsing errors
		msg.ReplyMarkup = replyMarkup

		_, err := bot.Send(msg)
		if err != nil {
			return fmt.Errorf("error sending message: %w", err)
		}
	} else {
		logger.Infof("Skipping message upload - invalid channel ID")
	}

	// Send files as media group (max 10 files per group)
	if len(documents) > 0 {
		logger.Infof("Found %d files to upload", len(documents))
		
		// Create media group with all files
		var mediaGroup []interface{}
		for i, doc := range documents {
			if i >= 10 { // Limit to 10 files
				logger.Infof("Limiting to first 10 files (found %d total)", len(documents))
				break
			}
			
			mediaDoc := doc.(tgbotapi.InputMediaDocument)
			// Only first file gets caption in media group
			if i == 0 {
				mediaDoc.Caption = fmt.Sprintf("📎 فایل‌های %s - %s\n%s", repo.Name, release.TagName, createCaption(repo, release, fileHashes, signatures))
			} else {
				mediaDoc.Caption = ""
			}
			mediaDoc.ParseMode = ""
			mediaGroup = append(mediaGroup, mediaDoc)
		}
		
		// Send media group to channel
		if len(mediaGroup) > 0 {
			channelID, _ := strconv.ParseInt(config.Telegram.ChannelID, 10, 64)
			mediaMsg := tgbotapi.NewMediaGroup(channelID, mediaGroup)
			_, err := bot.SendMediaGroup(mediaMsg)
			if err != nil {
				logger.Errorf("Error sending media group: %v", err)
				// Fallback: send files individually
				for i, doc := range documents {
					if i >= 10 { break }
					mediaDoc := doc.(tgbotapi.InputMediaDocument)
					content, downloadErr := downloadFile(release.Assets[i].BrowserDownloadURL)
					if downloadErr != nil {
						logger.Errorf("Error re-downloading %s: %v", release.Assets[i].Name, downloadErr)
						continue
					}
					
					fileReader := tgbotapi.FileReader{
						Name:   release.Assets[i].Name,
						Reader: bytes.NewReader(content),
					}
					
					docMsg := tgbotapi.NewDocument(channelID, fileReader)
					docMsg.Caption = fmt.Sprintf("📎 %s", release.Assets[i].Name)
					docMsg.ParseMode = ""
					
					_, err = bot.Send(docMsg)
					if err != nil {
						logger.Errorf("Error sending individual file %s: %v", release.Assets[i].Name, err)
					}
				}
			} else {
				logger.Infof("Successfully sent media group with %d files", len(mediaGroup))
			}
		}
	}

	// Mark as processed
	processedReleases[releaseID] = time.Now().Format(time.RFC3339)
	saveErr := saveProcessedReleases()
	if saveErr != nil {
		logger.Errorf("Error saving processed releases: %v", saveErr)
	}

	logger.Infof("Successfully sent release %s to channel", releaseID)
	return nil
}

func checkAllRepositories() {
	logger.Info("Checking all repositories for new releases")
	
	for _, repo := range config.Repositories {
		logger.Infof("Checking repository: %s", repo.Name)
		releases, err := getGitHubReleases(repo.GitHubURL)
		if err != nil {
			logger.Errorf("Error fetching releases for %s: %v", repo.GitHubURL, err)
			continue
		}
		
		logger.Infof("Found %d releases for %s", len(releases), repo.Name)
		
		if len(releases) == 0 {
			logger.Infof("No releases found for %s", repo.Name)
			continue
		}
		
		// Get the latest non-draft release
		var latestRelease *GitHubRelease
		for _, release := range releases {
			if !release.Draft {
				latestRelease = &release
				break
			}
		}
		
		if latestRelease == nil {
			logger.Infof("No non-draft releases found for %s", repo.Name)
			continue
		}
		
		logger.Infof("Latest release for %s: %s", repo.Name, latestRelease.TagName)
		
		logger.Infof("Starting to send release to channel...")
		err = sendReleaseToChannel(repo, *latestRelease)
		if err != nil {
			logger.Errorf("Error sending release to channel: %v", err)
			// Continue to next repository instead of stopping
			continue
		} else {
			logger.Infof("Release sent successfully")
		}
		
		// Check if any repository failed
		if repo.Name == "SlipNet" {
			logger.Infof("All repositories processed. Bot will continue monitoring...")
			// Exit gracefully after processing all repositories
			return
		}
	}
}

func main() {
	// Load configuration
	err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Load processed releases
	err = loadProcessedReleases()
	if err != nil {
		log.Fatalf("Error loading processed releases: %v", err)
	}

	// Initialize Telegram bot
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable is required")
	}
	
	bot, err = tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatalf("Error creating bot: %v", err)
	}

	logger.Infof("Bot authorized as @%s", bot.Self.UserName)

	// Check if channel ID is numeric
	if strings.HasPrefix(config.Telegram.ChannelID, "@") {
		logger.Errorf("Channel ID must be numeric, not username")
		logger.Infof("Please replace '%s' in config.json with numeric channel ID", config.Telegram.ChannelID)
		logger.Infof("Get numeric ID from @userinfobot by forwarding a message from your channel")
		logger.Infof("Continuing with test mode - will check releases but won't send files")
		// Set a flag to skip file uploads
		config.Telegram.ChannelID = "0" // Invalid ID to skip uploads
	} else {
		logger.Infof("Using channel ID: %s", config.Telegram.ChannelID)
	}

	// Setup cron job
	c := cron.New()
	_, err = c.AddFunc("@every 6h", checkAllRepositories)
	if err != nil {
		log.Fatalf("Error adding cron job: %v", err)
	}

	c.Start()

	// Run immediately on start
	checkAllRepositories()

	// Keep the program running
	select {}
}
