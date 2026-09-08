#!/bin/bash
set -e

echo "📦 Bundling React app to single HTML artifact..."

# Check if we're in a project directory
if [ ! -f "package.json" ]; then
  echo "❌ Error: No package.json found. Run this script from your project root."
  exit 1
fi

# Check if index.html exists
if [ ! -f "index.html" ]; then
  echo "❌ Error: No index.html found in project root."
  echo "   This script requires an index.html entry point."
  exit 1
fi

# A failed rebuild must not leave an older deliverable looking current.
rm -f bundle.html .bundle-pending.html
trap 'rm -f .bundle-pending.html' EXIT

echo "Checking the complete TypeScript project and Vite build..."
pnpm run build

# Install bundling dependencies
echo "📦 Installing bundling dependencies..."
if [ ! -x node_modules/.bin/parcel ] || [ ! -x node_modules/.bin/html-inline ]; then
  pnpm add -D parcel@2.16.4 @parcel/config-default@2.16.4 parcel-resolver-tspaths@0.0.9 html-inline@1.2.0
fi

# Create Parcel config with tspaths resolver
if [ ! -f ".parcelrc" ]; then
  echo "🔧 Creating Parcel configuration with path alias support..."
  cat > .parcelrc << 'EOF'
{
  "extends": "@parcel/config-default",
  "resolvers": ["parcel-resolver-tspaths", "..."]
}
EOF
fi

# Clean previous build
echo "🧹 Cleaning previous build..."
rm -rf dist bundle.html

# Build with Parcel
echo "🔨 Building with Parcel..."
pnpm exec parcel build index.html --dist-dir dist --no-source-maps

# Inline everything into single HTML
echo "🎯 Inlining all assets into single HTML file..."
pnpm exec html-inline dist/index.html > .bundle-pending.html

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
python3 "$SCRIPT_DIR/verify-artifact.py" .bundle-pending.html
mv .bundle-pending.html bundle.html

# Get file size
FILE_SIZE=$(du -h bundle.html | cut -f1)

echo ""
echo "✅ Bundle complete!"
echo "📄 Output: bundle.html ($FILE_SIZE)"
echo ""
echo "Build and browser smoke checks passed. Inspect artifact-validation screenshots and test the requested interactions before send_file."
