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

class GitHubReleaseBot:
    def __init__(self):
        self.config = Config()
        self.processed_releases = {}
        self.client = None
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
            with open('processed_releases.json', 'w') as f:
                json.dump(self.processed_releases, f, indent=2)
        except Exception as e:
            logger.error(f"Error saving processed releases: {e}")
    
    def get_file_hash(self, content: bytes) -> str:
        """Calculate SHA256 hash of file content"""
        return hashlib.sha256(content).hexdigest()
    
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
            caption += f"🔗 GitHub: {repo.github_url}\\n"
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
        intro_caption = f"🚀 New Release: #{repo.name}\n\n📦 Version: {release.get('tag_name', 'N/A')}\n📅 Date: {release.get('published_at', 'N/A')}\n\n🔗 GitHub: {repo.github_url}"
        
        # Create inline keyboard
        channel_url = f"https://t.me/{channel_username}" if channel_username else f"https://t.me/c/{abs(channel_id)}"
        keyboard = [[Button.url("🔗 Github Mirror", url=channel_url)]]
        
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
        
        file_hashes = {}
        temp_file_paths = {}
        
        # Download and process each asset
        for asset in assets:
            asset_name = asset.get('name', 'unknown')
            download_url = asset.get('browser_download_url', '')
            
            if not download_url:
                logger.error(f"No download URL for asset: {asset_name}")
                continue
            
            logger.info(f"Downloading asset: {asset_name}")
            
            # Download file to temp
            try:
                import requests
                response = requests.get(download_url, stream=True)
                response.raise_for_status()
                
                import tempfile
                import os
                import hashlib
                hash_obj = hashlib.sha256()
                with tempfile.NamedTemporaryFile(delete=False) as temp_file:
                    for chunk in response.iter_content(chunk_size=8192):
                        temp_file.write(chunk)
                        hash_obj.update(chunk)
                    temp_file_path = temp_file.name
                
                file_hash = hash_obj.hexdigest()
                file_hashes[asset_name] = file_hash
                temp_file_paths[asset_name] = temp_file_path
                
            except Exception as e:
                logger.error(f"Error downloading {asset_name}: {e}")
                continue
        
        # Send files
        caption = self.create_caption(repo, release, file_hashes)
        
        for asset in assets:
            asset_name = asset.get('name', 'unknown')
            download_url = asset.get('browser_download_url', '')
            
            if not download_url:
                continue
            
            temp_file_path = temp_file_paths.get(asset_name)
            if not temp_file_path:
                continue
            
            try:
                # Send file from temp
                file_hash = file_hashes.get(asset_name, 'N/A')
                await self.client.send_file(
                    channel_id,
                    file=(temp_file_path, asset_name),
                    caption=f"📎 #{repo.name}\n📦 Version: {release.get('tag_name', 'N/A')}\n📎 File: `{asset_name}`\n🔒 SHA256: `{file_hash}`",
                    parse_mode='md'
                )
                
                logger.info(f"Successfully sent file: {asset_name}")
                
                # Add delay between uploads
                await asyncio.sleep(5)
                
                os.unlink(temp_file_path)
                
            except Exception as e:
                logger.error(f"Error sending file {asset_name}: {e}")
                
                # Send fallback message with download button
                size_mb = os.path.getsize(temp_file_path) // (1024 * 1024)
                fallback_msg = f"📎 File: `{asset_name}`\n\n📊 Size: {size_mb} MB\n\n⚠️ Download from GitHub:"
                
                keyboard = [[Button.url("📥 Download from GitHub", url=download_url)]]
                
                await self.client.send_message(
                    channel_id,
                    fallback_msg,
                    buttons=keyboard,
                    parse_mode='md'
                )
                os.unlink(temp_file_path)
                
                # Delay after fallback
                await asyncio.sleep(5)
        
        logger.info(f"Successfully sent release {release.get('tag_name', 'unknown')} for {repo.name}")
    
    async def check_all_repositories(self):
        """Check all repositories for new releases"""
        logger.info("Checking all repositories for new releases")
        
        for repo in self.config.repositories:
            logger.info(f"Checking repository: {repo.name}")
            
            try:
                releases = await self.get_github_releases(repo.github_url)
                
                if not releases:
                    logger.info(f"No releases found for {repo.name}")
                    continue
                
                # Get latest non-draft release
                latest_release = None
                for release in releases:
                    if not release.get('draft', False):
                        latest_release = release
                        break
                
                if not latest_release:
                    logger.info(f"No non-draft releases found for {repo.name}")
                    continue
                
                tag = latest_release.get('tag_name', '')
                stored_tag = self.processed_releases.get(repo.name, '')
                if self.is_newer_version(tag, stored_tag):
                    logger.info(f"Latest release for {repo.name}: {tag}")
                    await self.send_release_to_channel(repo, latest_release)
                    self.processed_releases[repo.name] = tag
                    self.save_processed_releases()
                else:
                    logger.info(f"No new release for {repo.name}, latest is {tag}, stored is {stored_tag}")
                
            except Exception as e:
                logger.error(f"Error checking {repo.name}: {e}")
                continue
    
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
            await self.check_all_repositories()
            logger.info("Initial check completed")
            
        except Exception as e:
            logger.error(f"Error running bot: {e}")
        finally:
            await self.client.disconnect()

if __name__ == "__main__":
    bot = GitHubReleaseBot()
    asyncio.run(bot.run())
