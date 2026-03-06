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
	}
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

var (
	config           Config
	bot             *tgbotapi.BotAPI
	processedReleases map[string]string
	logger          *logrus.Logger
)

func init() {
	// Initialize logger
	logger = logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
}

func loadConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("error reading config file: %w", err)
	}

	err = json.Unmarshal(data, &config)
	if err != nil {
		return fmt.Errorf("error parsing config: %w", err)
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
		return fmt.Errorf("error saving processed releases: %w", err)
	}

	return nil
}

func loadProcessedReleases() error {
	data, err := os.ReadFile("processed_releases.json")
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, create empty map
			processedReleases = make(map[string]string)
			return nil
		}
		return fmt.Errorf("error reading processed releases: %w", err)
	}

	err = json.Unmarshal(data, &processedReleases)
	if err != nil {
		return fmt.Errorf("error parsing processed releases: %w", err)
	}

	return nil
}

func getFileHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func verifyPGPSignature() (bool, error) {
	// PGP verification disabled due to library compatibility issues
	// In production, you'd need proper GPG setup
	// For now, we'll just return true if signature exists
	return false, nil
}

func createCaption(repo Repository, release GitHubRelease, fileHashes map[string]string) string {
	var caption strings.Builder
	
	caption.WriteString(fmt.Sprintf("🚀 ریلیز جدید: %s\n\n", repo.Name))
	caption.WriteString(fmt.Sprintf("📦 نسخه: %s\n", release.TagName))
	caption.WriteString(fmt.Sprintf("📅 تاریخ: %s\n\n", release.PublishedAt.Format("2006-01-02 15:04:05")))

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
			caption.WriteString(fmt.Sprintf("📎 %s:\n`%s`\n", filename, hash))
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
	}

	// Create media group with all files
	var documents []interface{}
	for i, asset := range release.Assets {
		if i >= 10 { // Limit to 10 files
			logger.Infof("Limiting to first 10 files (found %d total)", len(release.Assets))
			break
		}
		
		content, downloadErr := downloadFile(asset.BrowserDownloadURL)
		if downloadErr != nil {
			logger.Errorf("Error re-downloading %s: %v", asset.Name, downloadErr)
			continue
		}
		
		// Create file reader for upload
		fileReader := tgbotapi.FileReader{
			Name:   asset.Name,
			Reader: bytes.NewReader(content),
		}
		
		// Create media document using NewInputMediaDocument
		mediaDoc := tgbotapi.NewInputMediaDocument(fileReader)
		
		// No caption for media group files - will be sent separately
		
		documents = append(documents, mediaDoc)
	}
	
	// Send introduction message first
	if config.Telegram.ChannelID != "0" {
		// Create simple introduction message (English)
		introCaption := fmt.Sprintf("🚀 New Release: %s\n\n📦 Version: %s\n📅 Date: %s\n\n🔗 GitHub: %s", 
			repo.Name, release.TagName, release.PublishedAt.Format("2006-01-02 15:04:05"), repo.GitHubURL)
		
		// Add hashes to introduction
		if len(fileHashes) > 0 {
			introCaption += "\n\n🔒 SHA256 Hashes:"
			var filenames []string
			for filename := range fileHashes {
				filenames = append(filenames, filename)
			}
			sort.Strings(filenames)
			
			for _, filename := range filenames {
				hash := fileHashes[filename]
				introCaption += fmt.Sprintf("\n📎 %s: `%s`", filename, hash)
			}
		}
		
		// Create inline keyboard with back button
		channelURL := fmt.Sprintf("https://t.me/%s", strings.TrimPrefix(config.Telegram.ChannelID, "@"))
		keyboard := [][]tgbotapi.InlineKeyboardButton{
			{
				{Text: "🔙 Back to Channel", URL: &channelURL},
			},
		}
		replyMarkup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

		msg := tgbotapi.NewMessageToChannel(config.Telegram.ChannelID, introCaption)
		msg.ParseMode = "Markdown" // Enable Markdown for code formatting
		msg.ReplyMarkup = replyMarkup

		_, err := bot.Send(msg)
		if err != nil {
			logger.Errorf("Error sending introduction message: %v", err)
		} else {
			logger.Infof("Successfully sent introduction message")
		}
	} else {
		logger.Infof("Skipping message upload - invalid channel ID")
	}

	// Send files individually (no media group)
	if len(documents) > 0 {
		channelID, _ := strconv.ParseInt(config.Telegram.ChannelID, 10, 64)
		
		for i, doc := range documents {
			mediaDoc := doc.(tgbotapi.InputMediaDocument)
			fileReader := mediaDoc.Media.(tgbotapi.FileReader)
			fileName := fileReader.Name
			
			// Simple caption for individual files (English)
			var caption string
			if i == 0 {
				caption = fmt.Sprintf("📎 %s - %s\n\n📦 Version: %s\n📎 File: %s", 
					repo.Name, release.TagName, release.TagName, fileName)
			} else {
				caption = fmt.Sprintf("📎 %s", fileName)
			}
			
			// Add hash if available
			if hash, exists := fileHashes[fileName]; exists {
				caption += fmt.Sprintf("\n🔒 SHA256: `%s`", hash)
			}
			
			// Find the corresponding asset to get download URL
			var downloadURL string
			for _, asset := range release.Assets {
				if asset.Name == fileName {
					downloadURL = asset.BrowserDownloadURL
					break
				}
			}
			
			if downloadURL == "" {
				logger.Errorf("Could not find download URL for file: %s", fileName)
				continue
			}
			
			// Create new file reader for each upload
			content, downloadErr := downloadFile(downloadURL)
			if downloadErr != nil {
				logger.Errorf("Error re-downloading %s: %v", fileName, downloadErr)
				continue
			}
			
			newFileReader := tgbotapi.FileReader{
				Name:   fileName,
				Reader: bytes.NewReader(content),
			}
			
			msg := tgbotapi.NewDocument(channelID, newFileReader)
			msg.Caption = caption
			msg.ParseMode = "Markdown" // Enable Markdown for code formatting
			_, sendErr := bot.Send(msg)
			if sendErr != nil {
				logger.Errorf("Error sending individual file %s: %v", fileName, sendErr)
			} else {
				logger.Infof("Successfully sent file: %s", fileName)
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
		
		// Get latest non-draft release
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

func getGitHubReleases(repoURL string) ([]GitHubRelease, error) {
	// Extract owner and repo from URL
	parts := strings.Split(repoURL, "/")
	if len(parts) < 5 {
		return nil, fmt.Errorf("invalid GitHub URL: %s", repoURL)
	}
	
	owner := parts[3]
	repoName := parts[4]
	
	// Get releases from GitHub API
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", owner, repoName)
	
	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("error fetching releases: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	var releases []GitHubRelease
	err = json.NewDecoder(resp.Body).Decode(&releases)
	if err != nil {
		return nil, fmt.Errorf("error parsing releases: %w", err)
	}

	return releases, nil
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

	return io.ReadAll(resp.Body)
}

func main() {
	// Load configuration
	err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	
	// Initialize processed releases
	processedReleases = make(map[string]string)
	
	// Load existing processed releases
	loadErr := loadProcessedReleases()
	if loadErr != nil {
		logger.Infof("No existing processed releases file found, starting fresh")
	}
	
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
	
	bot, err = tgbotapi.NewBotAPI(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if err != nil {
		log.Fatalf("Error creating bot: %v", err)
	}

	logger.Infof("Bot authorized as @%s", bot.Self.UserName)

	// Setup cron job
	c := cron.New()
	_, err = c.AddFunc("@every 6h", func() {
		logger.Info("Starting scheduled check...")
		checkAllRepositories()
		logger.Info("Check completed. Bot will exit after this run.")
		// Exit after one run instead of continuous monitoring
		os.Exit(0)
	})
	if err != nil {
		log.Fatalf("Error adding cron job: %v", err)
	}

	c.Start()

	// Run immediately on start
	logger.Info("Starting initial check...")
	checkAllRepositories()
	logger.Info("Initial check completed. Bot will exit.")
	os.Exit(0)
}
