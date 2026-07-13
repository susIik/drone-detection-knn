#!/usr/bin/env python3
"""
Rebuild Prototypes with PANNS Embeddings

This script regenerates the prototypes.json file using PANNS embeddings
instead of hand-crafted features. This provides much more discriminative
features for similar-sounding drones.

Usage:
    python rebuild_prototypes_panns.py --dir ../Drone-Training-Data --out ../server/drone/prototypes.json
"""

import os
import sys
import json
import argparse
import glob
from pathlib import Path
from embedding_service import embed_audio_panns, at
import hashlib


def generate_prototype_id(label):
    """Generate a unique ID for a prototype"""
    # Sanitize label
    safe_label = label.lower().replace(' ', '_')
    safe_label = ''.join(c for c in safe_label if c.isalnum() or c in ('_', '-'))
    
    if not safe_label:
        safe_label = "prototype"
    
    # Add random suffix
    random_hex = hashlib.md5(os.urandom(8)).hexdigest()[:8]
    return f"proto_{safe_label}_{random_hex}"


def infer_label_from_directory(dir_path):
    """Infer label from directory name"""
    base = os.path.basename(dir_path)
    label = base.lower().replace('_', ' ').replace('-', ' ')
    return label.strip()


def process_directory(root_dir, category="drone"):
    """Process all subdirectories and generate prototypes"""
    print(f"Processing directory: {root_dir}")
    
    # Find all subdirectories
    subdirs = [d for d in Path(root_dir).iterdir() if d.is_dir()]
    
    if not subdirs:
        print(f"No subdirectories found in {root_dir}")
        return []
    
    print(f"Found {len(subdirs)} subdirectories")
    
    all_prototypes = []
    
    for subdir in sorted(subdirs):
        label = infer_label_from_directory(str(subdir))
        print(f"\nProcessing: {subdir.name} -> label '{label}'")
        
        # Find all audio files
        audio_files = []
        for ext in ['*.wav', '*.mp3']: # add *.WAV *.MP3 if on linux
            audio_files.extend(glob.glob(os.path.join(subdir, ext)))
        
        if not audio_files:
            print(f"  No audio files found, skipping")
            continue
        
        print(f"  Found {len(audio_files)} audio files")
        
        for i, audio_path in enumerate(sorted(audio_files), 1):
            try:
                filename = os.path.basename(audio_path)
                print(f"  [{i}/{len(audio_files)}] Processing {filename}...", end=" ")
                
                # Generate embedding
                embedding = embed_audio_panns(audio_path)
                
                # Create prototype
                prototype = {
                    "id": generate_prototype_id(label),
                    "label": label,
                    "category": category,
                    "description": f"{label} from {filename}",
                    "source": audio_path,
                    "features": embedding.tolist()
                }
                
                all_prototypes.append(prototype)
                print("✓")
                
            except Exception as e:
                print(f"✗ ERROR: {e}")
                continue
    
    return all_prototypes


def main():
    parser = argparse.ArgumentParser(description='Rebuild prototypes with PANNS embeddings')
    parser.add_argument('--dir', required=True, help='Root directory containing subdirectories of audio files')
    parser.add_argument('--out', required=True, help='Output JSON file path')
    parser.add_argument('--category', default='drone', help='Category for all prototypes (default: drone)')
    
    args = parser.parse_args()
    
    # Check if model is loaded
    if at is None:
        print("ERROR: PANNS model failed to load")
        sys.exit(1)
    
    print("PANNS model loaded successfully")
    print(f"Using device: {'cuda' if at.device == 'cuda' else 'cpu'}")
    print()
    
    # Process directories
    prototypes = process_directory(args.dir, args.category)
    
    if not prototypes:
        print("\nERROR: No prototypes were created")
        sys.exit(1)
    
    # Write output
    os.makedirs(os.path.dirname(args.out), exist_ok=True)
    
    with open(args.out, 'w') as f:
        json.dump(prototypes, f, indent=2)
    
    print(f"\n✓ Successfully created {len(prototypes)} prototypes in {args.out}")
    
    # Print statistics
    from collections import Counter
    labels = Counter([p['label'] for p in prototypes])
    
    print("\nLabel distribution:")
    for label, count in sorted(labels.items()):
        print(f"  {label:20s}: {count} prototypes")
    
    categories = Counter([p['category'] for p in prototypes])
    print("\nCategory distribution:")
    for category, count in sorted(categories.items()):
        print(f"  {category:20s}: {count} prototypes")


if __name__ == '__main__':
    main()

