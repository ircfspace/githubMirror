#!/usr/bin/env python3

import os
import json
import hashlib
import asyncio
import logging
import time
from datetime import datetime
from typing import Dict, List, Optional
from packaging.version import Version

from bs4 import BeautifulSoup
import cloudscraper

from telethon import TelegramClient, events, Button
from telethon.tl.functions.messages import GetDialogsRequest
from telethon.tl.types import InputPeerEmpty
from telethon.tl.types import InputMediaDocument

# Load environment variables from .env file
def load_env():
    env_vars = {}
    if os.path.exists('.env'):
        with open('.env', 'r') as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith('#') and '=' in line:
                    key, value = line.split('=', 1)
                    env_vars[key.strip()] = value.strip()
    return env_vars

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

class Config:
    def __init__(self):
        self.telegram = {}
        self.repositories = []

class Repository:
    def __init__(self):
        self.name = ""
        self.github_url = ""
        self.google_play_url = ""
        self.apple_store_url = ""
        self.microsoft_store_url = ""

class APKMirror:
    def __init__(self, timeout: int = 5, results: int = 5):
        self.timeout = timeout
        self.results = results
        self.user_agent = "Mozilla/5.0 (X11; Linux x86_64; rv:122.0) Gecko/20100101 Firefox/122.0"
        self.headers = {"User-Agent": self.user_agent}
        self.base_url = "https://www.apkmirror.com"
        self.base_search = f"{self.base_url}/?post_type=app_release&searchtype=apk&s="
        self.scraper = cloudscraper.create_scraper()

    def search(self, query):
        time.sleep(self.timeout)
        search_url = self.base_search + query.replace('.', '+')
        resp = self.scraper.get(search_url, headers=self.headers)
        soup = BeautifulSoup(resp.text, "html.parser")
        apps = []
        appRow = soup.find_all("div", {"class": "appRow"})
        for app in appRow:
            try:
                app_dict = {
                    "name": app.find("h5", {"class": "appRowTitle"}).text.strip(),
                    "link": self.base_url + app.find("a", {"class": "downloadLink"})["href"],
                }
                apps.append(app_dict)
            except AttributeError:
                pass
        logger.info(f"APKMirror search for '{query}' found {len(apps)} results")
        return apps[:self.results]

    def get_latest_version_link(self, app_link):
        time.sleep(self.timeout)
        resp = self.scraper.get(app_link, headers=self.headers)
        soup = BeautifulSoup(resp.text, "html.parser")
        # The latest version is the first .appRow in the list
        appRow = soup.find("div", {"class": "appRow"})
        if appRow:
            return self.base_url + appRow.find("a", {"class": "downloadLink"})["href"]
        return None

    def get_app_details(self, app_download_link):
        time.sleep(self.timeout)
        try:
            resp = self.scraper.get(app_download_link, headers=self.headers)
            resp.raise_for_status()
        except Exception as e:
            logger.error(f"Failed to retrieve app details: {e}")
            return {}
        
        soup = BeautifulSoup(resp.text, "html.parser")
        
        table_rows = soup.find_all("div", {"class": ["table-row", "headerFont"]})
        if len(table_rows) < 2:
            logger.error("Failed to find table rows in app details page")
            return {}
        
        data = table_rows[1]
        
        cells = data.find_all("div", {"class": ["table-cell", "rowheight", "addseparator", "expand", "pad", "dowrap"]})
        if len(cells) < 3:
            logger.error("Failed to find cells in app details table")
            return {}
        
        try:
            architecture = cells[1].text.strip()
            android_version = cells[2].text.strip()
            dpi = cells[3].text.strip()
            download_link = self.base_url + data.find_all("a", {"class": "accent_color"})[0]["href"]
        except IndexError:
            logger.error("Failed to extract app details from cells")
            return {}
        
        return {
            "architecture": architecture,
            "android_version": android_version,
            "dpi": dpi,
            "download_link": download_link,
        }

    def get_download_link(self, app_download_link):
        time.sleep(self.timeout)
        resp = self.scraper.get(app_download_link, headers=self.headers)
        soup = BeautifulSoup(resp.text, "html.parser")
        return self.base_url + str(soup.find_all("a", {"class": "downloadButton"})[0]["href"])

    def get_direct_download_link(self, app_download_url):
        time.sleep(self.timeout)
        resp = self.scraper.get(app_download_url, headers=self.headers)
        soup = BeautifulSoup(resp.text, "html.parser")
        return soup.find("a", {"rel": "nofollow", "data-google-interstitial": "false"})["href"]

class GitHubReleaseBot:
    def __init__(self):
        self.config = Config()
        self.processed_releases = {}
        self.client = None
        self.apkmirror = APKMirror()
        self.load_config()
        self.load_processed_releases()
    
    def load_config(self):
        """Load configuration from config.json"""
        try:
            with open('config.json', 'r') as f:
                data = json.load(f)
                
            # Load telegram config
            self.config.telegram = data.get('telegram', {})
            
            # Load repositories
            self.config.repositories = []
            for repo_data in data.get('repositories', []):
                repo = Repository()
                repo.name = repo_data.get('name', '')
                repo.github_url = repo_data.get('github_url', '')
                repo.google_play_url = repo_data.get('google_play_url', '')
                repo.apple_store_url = repo_data.get('apple_store_url', '')
                repo.microsoft_store_url = repo_data.get('microsoft_store_url', '')
                self.config.repositories.append(repo)
                
            logger.info("Configuration loaded successfully")
            
        except Exception as e:
            logger.error(f"Error loading config: {e}")
            raise
    
    def load_processed_releases(self):
        """Load processed releases from file"""
        try:
            if os.path.exists('processed_releases.json'):
                with open('processed_releases.json', 'r') as f:
                    self.processed_releases = json.load(f)
            else:
                self.processed_releases = {}
                logger.info("No existing processed releases file found, starting fresh")
        except Exception as e:
            logger.error(f"Error loading processed releases: {e}")
            self.processed_releases = {}
    
    def save_processed_releases(self):
        """Save processed releases to file"""
        try:
            import os
            current_dir = os.getcwd()
            logger.info(f"Saving processed releases to {current_dir}/processed_releases.json")
            logger.info(f"Current processed_releases data: {self.processed_releases}")
            
            with open('processed_releases.json', 'w') as f:
                json.dump(self.processed_releases, f, indent=2)
            
            logger.info("Successfully saved processed releases")
            
            # Verify file was written
            if os.path.exists('processed_releases.json'):
                file_size = os.path.getsize('processed_releases.json')
                logger.info(f"processed_releases.json exists, size: {file_size} bytes")
            else:
                logger.error("processed_releases.json was not created!")
                
        except Exception as e:
            logger.error(f"Error saving processed releases: {e}")
            import traceback
            logger.error(f"Traceback: {traceback.format_exc()}")
    
    def get_file_hash(self, content: bytes) -> str:
        """Calculate SHA256 hash of file content"""
        return hashlib.sha256(content).hexdigest()
    
    def extract_package_name(self, url: str) -> str:
        """Extract package name from Google Play URL"""
        from urllib.parse import urlparse, parse_qs
        parsed = urlparse(url)
        query = parse_qs(parsed.query)
        return query.get('id', [''])[0]
    
    def is_newer_version(self, new_tag: str, old_tag: str) -> bool:
        if not old_tag:
            return True
        try:
            return Version(new_tag) > Version(old_tag)
        except:
            return new_tag != old_tag
    
    def create_caption(self, repo: Repository, release: dict, file_hashes: Dict[str, str]) -> str:
        """Create caption for release"""
        caption = f"🚀 ریلیز جدید: {repo.name}\\n\\n"
        caption += f"📦 نسخه: {release.get('tag_name', 'N/A')}\\n"
        caption += f"📅 تاریخ: {release.get('published_at', 'N/A')}\\n\\n"
        
        if repo.github_url:
            caption += f"🔗 Github: {repo.github_url}\\n"
        if repo.google_play_url:
            caption += f"🤖 Google Play: {repo.google_play_url}\\n"
        if repo.apple_store_url:
            caption += f"💰 App Store: {repo.apple_store_url}\\n"
        if repo.microsoft_store_url:
            caption += f"🪟 Microsoft Store: {repo.microsoft_store_url}\\n"
        
        if file_hashes:
            caption += "\\n🔒 هش‌های SHA256:\\n"
            for filename, hash_value in sorted(file_hashes.items()):
                caption += f"📎 {filename}:\\n`{hash_value}`\\n"
        
        return caption
    
    async def send_release_to_channel(self, repo: Repository, release: dict):
        """Send release to channel"""
        # Get channel info
        channel_id = self.config.telegram.get('channel_id')
        channel_username = self.config.telegram.get('channel_username', '').lstrip('@')
        
        if not channel_id:
            logger.error("No channel ID configured")
            return
        
        try:
            channel_id = int(channel_id)
        except ValueError:
            logger.error("Channel ID must be numeric")
            return
        
        # Send introduction message
        if repo.github_url:
            intro_caption = f"🚀 New Release: #{repo.name}\n\n📦 Version: {release.get('tag_name', 'N/A')}\n📅 Date: {release.get('published_at', 'N/A')}\n\n🔗 Github: {repo.github_url}"
            button_text = "🔗 Github Mirror"
        elif repo.google_play_url:
            intro_caption = f"🚀 New Release: #{repo.name}\n\n📦 Version: {release.get('tag_name', 'N/A')}\n📅 Date: {release.get('published_at', 'N/A')}\n\n🤖 Google Play: {repo.google_play_url}"
            button_text = "🤖 Google Play Mirror"
        else:
            intro_caption = f"🚀 New Release: #{repo.name}\n\n📦 Version: {release.get('tag_name', 'N/A')}\n📅 Date: {release.get('published_at', 'N/A')}"
            button_text = "🔗 Mirror"
        
        # Create inline keyboard
        channel_url = f"https://t.me/{channel_username}" if channel_username else f"https://t.me/c/{abs(channel_id)}"
        keyboard = [[Button.url(button_text, url=channel_url)]]
        
        await self.client.send_message(
            channel_id,
            intro_caption,
            buttons=keyboard
        )
        
        logger.info("Successfully sent introduction message")
        
        # Delay to avoid rate limits
        await asyncio.sleep(5)
        
        # Process assets
        assets = release.get('assets', [])
        if not assets:
            logger.info("No assets found in release")
            return
        
        logger.info(f"Found {len(assets)} assets in release")
        
        # Process each asset individually
        for asset in assets:
            asset_name = asset.get('name', 'unknown')
            download_url = asset.get('browser_download_url', '')
            
            if not download_url:
                logger.error(f"No download URL for asset: {asset_name}")
                continue
            
            logger.info(f"Processing asset: {asset_name}")
            
            # Download file to temp
            try:
                import requests
                response = requests.get(download_url, stream=True)
                response.raise_for_status()
                
                import tempfile
                import os
                import hashlib
                hash_obj = hashlib.sha256()
                with tempfile.NamedTemporaryFile(delete=False, dir=os.getcwd()) as temp_file:
                    total_size = int(response.headers.get('content-length', 0)) if response.headers.get('content-length') else 0
                    downloaded = 0
                    last_percent = 0
                    logger.info(f"Starting download: {asset_name} (Size: {total_size // (1024*1024)} MB)")
                    for chunk in response.iter_content(chunk_size=8192):
                        temp_file.write(chunk)
                        hash_obj.update(chunk)
                        downloaded += len(chunk)
                        if total_size > 0:
                            percent = (downloaded / total_size) * 100
                            if percent - last_percent >= 5 or percent >= 100:
                                logger.info(f"Downloading {asset_name}: [{'#' * int(percent // 5)}{' ' * (20 - int(percent // 5))}] {percent:.1f}%")
                                last_percent = percent
                    temp_file_path = temp_file.name
                
                file_hash = hash_obj.hexdigest()
                
                # Send file immediately after download
                try:
                    logger.info(f"Attempting to send file: {temp_file_path} as {asset_name}")
                    logger.info(f"File size: {os.path.getsize(temp_file_path)} bytes")
                    logger.info(f"Starting upload to Telegram...")
                    
                    # Create progress callback for upload
                    def upload_progress(current, total):
                        if total > 0:
                            percent = (current / total) * 100
                            if percent % 5 == 0 and percent > 0:  # Log every 5%
                                logger.info(f"Uploading {asset_name}: [{'=' * int(percent // 5)}{' ' * (20 - int(percent // 5))}] {percent:.1f}%")
                    
                    # First upload the file to get a file handle
                    logger.info(f"Uploading file with upload_file method...")
                    uploaded_file = await self.client.upload_file(
                        temp_file_path,
                        file_name=asset_name,
                        progress_callback=upload_progress
                    )
                    logger.info(f"File uploaded successfully: {uploaded_file}")
                    
                    # Create inline keyboard for attached files
                    channel_url = f"https://t.me/{channel_username}" if channel_username else f"https://t.me/c/{abs(channel_id)}"
                    if repo.github_url:
                        download_text = "📥 Download from Github"
                    elif repo.google_play_url:
                        download_text = "📥 Download from APKMirror"
                    else:
                        download_text = "📥 Download"
                    keyboard = [[Button.url(download_text, url=download_url)], [Button.url(button_text, url=channel_url)]]
                    
                    # Then send the file using the handle
                    logger.info(f"Sending file with send_file method...")
                    await self.client.send_file(
                        channel_id,
                        file=uploaded_file,
                        caption=f"#{repo.name}\n📦 Version: {release.get('tag_name', 'N/A')}\n📎 File: `{asset_name}`\n🔒 SHA256: `{file_hash}`",
                        buttons=keyboard,
                        parse_mode='md'
                    )
                    
                    logger.info(f"Successfully sent file: {asset_name}")
                    
                    # Add delay between uploads
                    await asyncio.sleep(5)
                    
                    os.unlink(temp_file_path)
                    
                except Exception as e:
                    logger.error(f"Error sending file {asset_name}: {e}", exc_info=True)
                    # Send fallback message with download button
                    size_mb = os.path.getsize(temp_file_path) // (1024 * 1024)
                    fallback_msg = f"📎 File: `{asset_name}`\n\n📊 Size: {size_mb} MB\n\n⚠️ Download from {'GitHub' if repo.github_url else 'APKMirror'}:"
                    
                    keyboard = [[Button.url(download_text, url=download_url)], [Button.url(button_text, url=download_url)]]
                    
                    await self.client.send_message(
                        channel_id,
                        fallback_msg,
                        buttons=keyboard,
                        parse_mode='md'
                    )
                    os.unlink(temp_file_path)
                    
                    # Delay after fallback
                    await asyncio.sleep(5)
                
            except Exception as e:
                logger.error(f"Error downloading {asset_name}: {e}")
                continue
        
        logger.info(f"Successfully sent release {release.get('tag_name', 'unknown')} for {repo.name}")
    
    async def send_summary_message(self):
        """Send summary message with list of supported programs"""
        # Get channel info
        channel_id = self.config.telegram.get('channel_id')
        channel_username = self.config.telegram.get('channel_username', '').lstrip('@')
        
        if not channel_id:
            logger.error("No channel ID configured for summary message")
            return
        
        try:
            channel_id = int(channel_id)
        except ValueError:
            logger.error("Channel ID must be numeric")
            return
        
        # Build message text
        message_text = "#گزارش\n"
        message_text += "وضعیت آخرین بروزرسانی برنامه‌ها مورد بررسی قرار گرفت.\n\n"
        message_text += "پروژه‌های پشتیبانی شده:\n"
        
        # Add each repository with its last processed version
        for repo in self.config.repositories:
            last_version = self.processed_releases.get(repo.name, 'نامشخص')
            message_text += f"#{repo.name}: {last_version}\n"
        
        # Create buttons
        channel_url = f"https://t.me/{channel_username}" if channel_username else f"https://t.me/c/{abs(channel_id)}"
        keyboard = [
            [Button.url("🌐 اینترنت آزاد برای همه", "https://t.me/ircfspace")],
            [Button.url("⚙️ کانفیگ فیلترشکن رایگان", "https://t.me/persianvpnhub")],
            [Button.url("📦 میرور گیت‌هاب", channel_url)]
        ]
        
        try:
            await self.client.send_message(
                channel_id,
                message_text,
                buttons=keyboard
            )
            logger.info("Summary message sent successfully")
            
            # Small delay after summary
            await asyncio.sleep(3)
            
        except Exception as e:
            logger.error(f"Error sending summary message: {e}")
    
    async def check_all_repositories(self):
        """Check all repositories for new releases"""
        logger.info("Checking all repositories for new releases")
        
        had_new_releases = False
        
        for repo in self.config.repositories:
            logger.info(f"Checking repository: {repo.name}")
            
            try:
                latest_release = None
                if repo.github_url:
                    releases = await self.get_github_releases(repo.github_url)
                    
                    if not releases:
                        logger.info(f"No releases found for {repo.name}")
                        continue
                    
                    # Get latest non-draft release
                    for release in releases:
                        if not release.get('draft', False):
                            latest_release = release
                            break
                    
                    if not latest_release:
                        logger.info(f"No non-draft releases found for {repo.name}")
                        continue
                elif repo.google_play_url:
                    latest_release = await self.get_apkmirror_release(repo)
                    if not latest_release:
                        logger.info(f"No release found for {repo.name}")
                        continue
                else:
                    logger.info(f"No GitHub or Google Play URL for {repo.name}")
                    continue
                
                tag = latest_release.get('tag_name', '')
                stored_tag = self.processed_releases.get(repo.name, '')
                if self.is_newer_version(tag, stored_tag):
                    logger.info(f"Latest release for {repo.name}: {tag}")
                    await self.send_release_to_channel(repo, latest_release)
                    self.processed_releases[repo.name] = tag
                    self.save_processed_releases()
                    had_new_releases = True
                else:
                    logger.info(f"No new release for {repo.name}, latest is {tag}, stored is {stored_tag}")
                
            except Exception as e:
                logger.error(f"Error checking {repo.name}: {e}")
                continue
        
        return had_new_releases
    
    async def get_github_releases(self, github_url: str) -> List[dict]:
        """Get releases from GitHub API"""
        try:
            import requests
            
            # Extract owner and repo from URL
            parts = github_url.strip('/').split('/')
            if len(parts) < 5:
                raise ValueError(f"Invalid GitHub URL: {github_url}")
            
            owner = parts[3]
            repo_name = parts[4]
            
            api_url = f"https://api.github.com/repos/{owner}/{repo_name}/releases"
            
            headers = {
                'User-Agent': 'GitHub-Release-Bot/1.0',
                'Accept': 'application/vnd.github.v3+json'
            }
            
            for attempt in range(3):
                try:
                    response = requests.get(api_url, headers=headers, timeout=30)
                    response.raise_for_status()
                    releases = response.json()
                    return releases
                except requests.exceptions.RequestException as e:
                    logger.warning(f"Attempt {attempt + 1} failed: {e}")
                    if attempt < 2:
                        time.sleep(5)
            
            # If all attempts failed
            raise Exception("Failed to fetch releases after 3 attempts")
            
        except Exception as e:
            logger.error(f"Error fetching releases: {e}")
            return []
    
    async def get_apkmirror_release(self, repo: Repository) -> dict:
        """Get latest release from APKMirror"""
        try:
            package_name = self.extract_package_name(repo.google_play_url)
            if not package_name:
                logger.error(f"Could not extract package name from {repo.google_play_url}")
                return {}
            
            # Search for the app
            apps = self.apkmirror.search(package_name)
            if not apps and repo.name:
                logger.info(f"No apps found for package {package_name}, trying name {repo.name}")
                apps = self.apkmirror.search(repo.name)
            
            if not apps:
                logger.error(f"No apps found for {package_name} or {repo.name}")
                return {}
            
            app_link = apps[0]['link']  # Take the first result
            
            # Get latest version link
            version_link = self.apkmirror.get_latest_version_link(app_link)
            if not version_link:
                logger.error(f"No version link found for {repo.name}")
                return {}
            
            # Extract version from link
            version = version_link.split('/')[-1].replace('-release', '').replace('-', '.')
            
            # Get download link for universal APK
            details = self.apkmirror.get_app_details(version_link)
            if not details:
                logger.error(f"Failed to get app details for {repo.name}")
                return {}
            
            if details.get('architecture') != 'universal':
                logger.warning(f"No universal APK found for {repo.name}, architecture: {details.get('architecture')}")
                # For now, return empty, but perhaps download anyway
                return {}
            
            download_link = self.apkmirror.get_download_link(details['download_link'])
            direct_link = self.apkmirror.get_direct_download_link(download_link)
            
            # Create release dict
            release = {
                'tag_name': version,
                'published_at': datetime.now().isoformat(),
                'assets': [{
                    'name': f'{package_name}.apk',
                    'browser_download_url': direct_link
                }]
            }
            return release
            
        except Exception as e:
            logger.error(f"Error fetching APKMirror release for {repo.name}: {e}")
            return {}
    
    async def run(self):
        """Run the bot"""
        # Load environment variables from .env file first
        env_vars = load_env()
        
        # Get credentials from environment variables or .env file
        api_id = int(env_vars.get('TELEGRAM_API_ID', os.getenv('TELEGRAM_API_ID', '0')))
        api_hash = env_vars.get('TELEGRAM_API_HASH', os.getenv('TELEGRAM_API_HASH', ''))
        bot_token = env_vars.get('TELEGRAM_BOT_TOKEN', os.getenv('TELEGRAM_BOT_TOKEN', ''))
        
        if not all([api_id, api_hash, bot_token]):
            logger.error("Missing required environment variables")
            logger.info("Please set TELEGRAM_API_ID, TELEGRAM_API_HASH, and TELEGRAM_BOT_TOKEN")
            return
        
        # Create client
        self.client = TelegramClient('bot_session', api_id, api_hash)
        
        try:
            await self.client.start(bot_token=bot_token)
            logger.info("Bot started successfully")
            
            # Run immediately
            had_new_releases = await self.check_all_repositories()
            logger.info("All repositories checked successfully - Bot execution completed")
            
            # Send summary message only if there were new releases
            if had_new_releases:
                await self.send_summary_message()
            else:
                logger.info("No new releases found, skipping summary message")
            
            logger.info("Exiting gracefully...")
            
        except Exception as e:
            logger.error(f"Error running bot: {e}")
            raise  # Re-raise to ensure non-zero exit code on error
        finally:
            await self.client.disconnect()
            logger.info("Bot disconnected and shut down")

if __name__ == "__main__":
    bot = GitHubReleaseBot()
    asyncio.run(bot.run())
