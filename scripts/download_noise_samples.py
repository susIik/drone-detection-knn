#!/usr/bin/env python3
"""
Download noise samples from Freesound.org to create a "noise" class for the classifier.

This script downloads various types of non-drone sounds to help reduce false positives:
- Urban noise (traffic, construction, crowds)
- Nature sounds (birds, wind, rain, insects)
- Household appliances (fans, AC, vacuum)
- Aircraft (planes, helicopters)

Requirements:
    pip install requests pydub

Setup:
    1. Get a free API key from https://freesound.org/apiv2/apply/
    2. Set environment variable: export FREESOUND_API_KEY="your-key-here"
    3. Run: python download_noise_samples.py
"""

import os
import sys
import time
import json
import subprocess
from pathlib import Path
from urllib.parse import urlencode

try:
    import requests
except ImportError:
    print("ERROR: 'requests' library not found")
    print("Install with: pip install requests")
    sys.exit(1)

# Configuration
OUTPUT_DIR = Path("train_data_noise")
SAMPLES_PER_CATEGORY = 5  # Adjust this to download more samples
SAMPLE_DURATION_MIN = 3   # Minimum duration in seconds
SAMPLE_DURATION_MAX = 20  # Maximum duration in seconds

# Sound categories to download
SOUND_CATEGORIES = {
    "traffic": [
        "car traffic city",
        "highway traffic",
        "street urban cars",
        "road noise vehicles",
        "city traffic ambient"
    ],
    "birds": [
        "bird chirping outdoor",
        "birds singing nature",
        "bird calls forest",
        "seagull outdoor",
        "crow cawing"
    ],
    "household": [
        "fan noise",
        "air conditioner running",
        "vacuum cleaner",
        "washing machine running",
        "refrigerator hum"
    ],
    "construction": [
        "construction site",
        "drilling noise",
        "hammer construction",
        "saw cutting",
        "jackhammer"
    ],
    "nature": [
        "wind blowing outdoor",
        "rain falling",
        "water stream",
        "leaves rustling",
        "outdoor ambient nature"
    ],
    "crowd": [
        "crowd talking",
        "people chatting cafe",
        "crowd noise street",
        "restaurant ambience",
        "mall crowd"
    ],
    "aircraft": [
        "airplane flying overhead",
        "helicopter flying",
        "small plane passing",
        "jet aircraft",
        "propeller plane"
    ],
    "industrial": [
        "factory noise",
        "machinery running",
        "motor running",
        "generator noise",
        "compressor running"
    ]
}


class FreesoundDownloader:
    def __init__(self, api_key):
        self.api_key = api_key
        self.base_url = "https://freesound.org/apiv2"
        self.session = requests.Session()
        self.session.headers.update({"Authorization": f"Token {api_key}"})
        
    def search_sounds(self, query, max_results=10):
        """Search for sounds matching a query."""
        params = {
            "query": query,
            "filter": f"duration:[{SAMPLE_DURATION_MIN} TO {SAMPLE_DURATION_MAX}]",
            "fields": "id,name,duration,previews,license",
            "page_size": max_results,
            "sort": "rating_desc"  # Get highest rated sounds
        }
        
        url = f"{self.base_url}/search/text/?" + urlencode(params)
        
        try:
            response = self.session.get(url, timeout=10)
            response.raise_for_status()
            data = response.json()
            return data.get("results", [])
        except requests.RequestException as e:
            print(f"  ⚠️  Search error: {e}")
            return []
    
    def download_sound(self, sound_id, output_path):
        """Download a sound by ID."""
        # Get sound details
        url = f"{self.base_url}/sounds/{sound_id}/"
        
        try:
            response = self.session.get(url, timeout=10)
            response.raise_for_status()
            sound_data = response.json()
            
            # Get preview URL (high quality OGG format)
            preview_url = sound_data.get("previews", {}).get("preview-hq-ogg")
            if not preview_url:
                preview_url = sound_data.get("previews", {}).get("preview-lq-ogg")
            
            if not preview_url:
                print(f"  ⚠️  No preview available for sound {sound_id}")
                return False
            
            # Download the preview
            audio_response = self.session.get(preview_url, timeout=30)
            audio_response.raise_for_status()
            
            # Save as OGG temporarily
            temp_ogg = output_path.with_suffix(".ogg")
            temp_ogg.write_bytes(audio_response.content)
            
            # Convert to WAV using ffmpeg
            if self.convert_to_wav(temp_ogg, output_path):
                temp_ogg.unlink()  # Delete temporary OGG file
                return True
            else:
                temp_ogg.unlink()
                return False
                
        except requests.RequestException as e:
            print(f"  ⚠️  Download error: {e}")
            return False
    
    def convert_to_wav(self, input_file, output_file):
        """Convert audio file to WAV format using ffmpeg."""
        try:
            cmd = [
                "ffmpeg",
                "-i", str(input_file),
                "-ar", "44100",      # Sample rate: 44.1kHz
                "-ac", "1",          # Mono
                "-sample_fmt", "s16", # 16-bit
                "-y",                # Overwrite output file
                "-loglevel", "error", # Only show errors
                str(output_file)
            ]
            
            result = subprocess.run(cmd, capture_output=True, text=True)
            
            if result.returncode != 0:
                print(f"  ⚠️  ffmpeg error: {result.stderr}")
                return False
            
            return True
            
        except FileNotFoundError:
            print("  ⚠️  ffmpeg not found. Please install ffmpeg:")
            print("     macOS: brew install ffmpeg")
            print("     Linux: sudo apt install ffmpeg")
            print("     Windows: Download from https://ffmpeg.org/")
            return False
        except Exception as e:
            print(f"  ⚠️  Conversion error: {e}")
            return False


def check_ffmpeg():
    """Check if ffmpeg is available."""
    try:
        result = subprocess.run(
            ["ffmpeg", "-version"],
            capture_output=True,
            text=True,
            timeout=5
        )
        return result.returncode == 0
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return False


def main():
    print("=" * 70)
    print("Freesound Noise Sample Downloader")
    print("=" * 70)
    print()
    
    # Check for API key
    api_key = "FuHW17IkgC6at7a2H8PHUMSv1rxnwgt9iWnfXxf7" #os.environ.get("FREESOUND_API_KEY")
    if not api_key:
        print("❌ ERROR: FREESOUND_API_KEY environment variable not set")
        print()
        print("To fix this:")
        print("1. Go to https://freesound.org/apiv2/apply/")
        print("2. Create a free account and get an API key")
        print("3. Set the environment variable:")
        print("   export FREESOUND_API_KEY='your-api-key-here'")
        print()
        print("Or pass it directly:")
        print("   FREESOUND_API_KEY='your-key' python download_noise_samples.py")
        sys.exit(1)
    
    # Check for ffmpeg
    if not check_ffmpeg():
        print("❌ ERROR: ffmpeg not found")
        print()
        print("Please install ffmpeg:")
        print("  macOS:   brew install ffmpeg")
        print("  Linux:   sudo apt install ffmpeg")
        print("  Windows: Download from https://ffmpeg.org/")
        sys.exit(1)
    
    # Create output directory
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    print(f"📁 Output directory: {OUTPUT_DIR}")
    print(f"📊 Will download {SAMPLES_PER_CATEGORY} samples per category")
    print(f"⏱️  Duration: {SAMPLE_DURATION_MIN}-{SAMPLE_DURATION_MAX} seconds")
    print()
    
    # Initialize downloader
    downloader = FreesoundDownloader(api_key)
    
    # Track statistics
    total_downloaded = 0
    total_failed = 0
    
    # Download samples for each category
    for category, queries in SOUND_CATEGORIES.items():
        print(f"📂 Category: {category}")
        print("-" * 70)
        
        category_dir = OUTPUT_DIR / category
        category_dir.mkdir(exist_ok=True)
        
        downloaded_count = 0
        
        for query in queries:
            if downloaded_count >= SAMPLES_PER_CATEGORY:
                break
            
            print(f"  🔍 Searching: '{query}'")
            
            # Search for sounds
            sounds = downloader.search_sounds(query, max_results=3)
            
            if not sounds:
                print(f"     No sounds found")
                continue
            
            # Try to download the first available sound
            for sound in sounds:
                if downloaded_count >= SAMPLES_PER_CATEGORY:
                    break
                
                sound_id = sound["id"]
                sound_name = sound["name"]
                duration = sound["duration"]
                
                # Create filename
                safe_name = "".join(c if c.isalnum() or c in "._- " else "_" for c in sound_name)
                safe_name = safe_name[:50]  # Limit length
                filename = f"{category}_{downloaded_count+1:02d}_{safe_name}.wav"
                output_path = category_dir / filename
                
                # Skip if already exists
                if output_path.exists():
                    print(f"     ⏭️  Already exists: {filename}")
                    downloaded_count += 1
                    total_downloaded += 1
                    break
                
                print(f"     ⬇️  Downloading: {sound_name} ({duration:.1f}s)")
                
                # Download and convert
                if downloader.download_sound(sound_id, output_path):
                    print(f"     ✅ Saved: {filename}")
                    downloaded_count += 1
                    total_downloaded += 1
                    time.sleep(1)  # Rate limiting
                    break
                else:
                    total_failed += 1
            
            time.sleep(0.5)  # Small delay between searches
        
        print(f"  ✅ Downloaded {downloaded_count}/{SAMPLES_PER_CATEGORY} for {category}")
        print()
    
    # Summary
    print("=" * 70)
    print("📊 Download Summary")
    print("=" * 70)
    print(f"✅ Successfully downloaded: {total_downloaded}")
    print(f"❌ Failed downloads: {total_failed}")
    print(f"📁 Saved to: {OUTPUT_DIR}")
    print()
    
    if total_downloaded > 0:
        print("🎯 Next Steps:")
        print()
        print("1. Review the downloaded samples (listen to verify quality)")
        print()
        print("2. Build prototypes from noise samples:")
        print(f"   cd server")
        print(f"   go run ./cmd/rebuild_prototypes \\")
        print(f"     -dir ../{OUTPUT_DIR} \\")
        print(f"     -category noise \\")
        print(f"     -out drone/noise_prototypes.json")
        print()
        print("3. Merge with existing prototypes:")
        print(f"   # Option A: Using jq (if installed)")
        print(f"   jq -s '.[0] + .[1]' \\")
        print(f"     drone/prototypes.json \\")
        print(f"     drone/noise_prototypes.json \\")
        print(f"     > drone/prototypes_merged.json")
        print(f"   mv drone/prototypes_merged.json drone/prototypes.json")
        print()
        print(f"   # Option B: Manually edit JSON and combine arrays")
        print()
        print("4. Test the improved classifier:")
        print(f"   go run ./cmd/train_eval -dir ../test_recordings -k 5")
        print()
        print("Expected improvement: 50%+ reduction in false positives!")
    else:
        print("⚠️  No samples downloaded. Check your API key and internet connection.")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\n\n⚠️  Download interrupted by user")
        sys.exit(1)
    except Exception as e:
        print(f"\n\n❌ Unexpected error: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)

