#!/bin/bash
set -e

# Define variables
GITHUB_REPO="campbel/tiny-tunnel"
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="tnl"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Print banner
echo -e "${BLUE}"
echo "┌─────────────────────────────────────┐"
echo "│             tnl installer           │"
echo "└─────────────────────────────────────┘"
echo -e "${NC}"

# Get the latest release info from GitHub
echo -e "${BLUE}🔍 Fetching latest release...${NC}"
API_URL="https://api.github.com/repos/$GITHUB_REPO/releases/latest"
if command -v curl &> /dev/null; then
    RELEASE_DATA=$(curl -s $API_URL)
elif command -v wget &> /dev/null; then
    RELEASE_DATA=$(wget -q -O- $API_URL)
else
    echo -e "${RED}❌ Error: Neither curl nor wget found. Please install one of them and try again.${NC}"
    exit 1
fi

# Extract version
VERSION=$(echo $RELEASE_DATA | grep -o '"tag_name":"[^"]*' | sed 's/"tag_name":"//')
if [ -z "$VERSION" ]; then
    echo -e "${RED}❌ Error: Unable to fetch the latest version. Please check your internet connection and try again.${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Latest version: $VERSION${NC}"

# Determine OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case $OS in
    darwin) OS="darwin" ;;
    linux) OS="linux" ;;
    *)
        echo -e "${RED}❌ Error: Unsupported operating system: $OS${NC}"
        echo -e "${YELLOW}tnl is available for macOS (darwin) and Linux.${NC}"
        exit 1
        ;;
esac

ARCH=$(uname -m)
case $ARCH in
    x86_64) ARCH="amd64" ;;
    amd64) ARCH="amd64" ;;
    arm64 | aarch64) ARCH="arm64" ;;
    *)
        echo -e "${RED}❌ Error: Unsupported architecture: $ARCH${NC}"
        echo -e "${YELLOW}tnl is available for amd64 and arm64 architectures.${NC}"
        exit 1
        ;;
esac

echo -e "${BLUE}🖥️  Detected: $OS/$ARCH${NC}"

# Construct download URL
BINARY_URL=""
ASSET_NAME="tnl-$OS-$ARCH"
ASSET_URLS=$(echo $RELEASE_DATA | grep -o '"browser_download_url":"[^"]*')
for url in $ASSET_URLS; do
    url=$(echo $url | sed 's/"browser_download_url":"//g')
    if [[ $url == *"$ASSET_NAME"* ]]; then
        BINARY_URL=$url
        break
    fi
done

if [ -z "$BINARY_URL" ]; then
    echo -e "${RED}❌ Error: No binary found for $OS/$ARCH${NC}"
    exit 1
fi

# Create temporary directory
TMP_DIR=$(mktemp -d)
BINARY_PATH="$TMP_DIR/$BINARY_NAME"

# Download the binary
echo -e "${BLUE}📥 Downloading tnl...${NC}"
if command -v curl &> /dev/null; then
    curl -sL "$BINARY_URL" -o "$BINARY_PATH"
elif command -v wget &> /dev/null; then
    wget -q -O "$BINARY_PATH" "$BINARY_URL"
fi

# Make binary executable
chmod +x "$BINARY_PATH"

# Check if installation requires sudo
if [ -w "$INSTALL_DIR" ]; then
    SUDO=""
else
    SUDO="sudo"
fi

# Install binary
echo -e "${BLUE}📦 Installing to $INSTALL_DIR/${BINARY_NAME}...${NC}"
$SUDO mv "$BINARY_PATH" "$INSTALL_DIR/$BINARY_NAME"

# Clean up
rm -rf "$TMP_DIR"

# Verify installation
if command -v $BINARY_NAME &> /dev/null; then
    echo -e "${GREEN}✅ tnl $VERSION has been successfully installed!${NC}"
    echo -e "${BLUE}Try it out with: ${YELLOW}$BINARY_NAME --help${NC}"
else
    echo -e "${RED}❌ Installation failed. The binary might not be in your PATH.${NC}"
    echo -e "${YELLOW}The binary was installed to: $INSTALL_DIR/$BINARY_NAME${NC}"
    exit 1
fi

# Add example usage
echo -e "\n${BLUE}Example usage:${NC}"
echo -e "${YELLOW}  # Start a tunnel server${NC}"
echo -e "  $BINARY_NAME serve"
echo -e "\n${YELLOW}  # Start a tunnel client${NC}"
echo -e "  $BINARY_NAME start myservice http://localhost:8080"
echo -e "\n${YELLOW}  # Update tnl to the latest version${NC}"
echo -e "  $BINARY_NAME update"