#!/bin/bash
set -e

# Define variables
GITHUB_REPO="campbel/tiny-tunnel"
BINARY_NAME="tnl"

# Determine installation directory
if [ -d "$HOME/.local/bin" ]; then
    INSTALL_DIR="$HOME/.local/bin"
elif [ -d "$HOME/bin" ]; then
    INSTALL_DIR="$HOME/bin"
else
    # Create ~/.local/bin if it doesn't exist
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
fi

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

# Extract version - Method 2 from test (more reliable)
VERSION=$(echo "$RELEASE_DATA" | grep "tag_name" | head -1 | awk -F': "' '{print $2}' | awk -F'",' '{print $1}')
if [ -z "$VERSION" ]; then
    echo -e "${RED}❌ Error: Unable to fetch the latest version. Please check your internet connection and try again.${NC}"
    echo -e "${YELLOW}API response (first 100 chars): ${NC}$(echo "$RELEASE_DATA" | head -c 100)"
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

# Construct download URL - Method 2 from test (more reliable)
BINARY_URL=""
ASSET_NAME="tnl-$OS-$ARCH"
echo -e "${BLUE}🔍 Looking for asset: $ASSET_NAME${NC}"

# Try using grep to extract the URL
ASSET_INFO=$(echo "$RELEASE_DATA" | grep -A 1 "\"name\": \"$ASSET_NAME\"")
if [ -n "$ASSET_INFO" ]; then
    BINARY_URL=$(echo "$ASSET_INFO" | grep "browser_download_url" | sed -E 's/.*"browser_download_url": "([^"]*)".*/\1/')
fi

# If that didn't work, try constructed URL
if [ -z "$BINARY_URL" ]; then
    echo -e "${YELLOW}Warning: Asset not found in release data, trying direct URL construction...${NC}"
    BINARY_URL="https://github.com/$GITHUB_REPO/releases/download/$VERSION/$ASSET_NAME"
    echo -e "${YELLOW}Using URL: $BINARY_URL${NC}"
fi

# Create temporary directory
TMP_DIR=$(mktemp -d)
BINARY_PATH="$TMP_DIR/$BINARY_NAME"

# Download the binary
echo -e "${BLUE}📥 Downloading tnl from $BINARY_URL...${NC}"
if command -v curl &> /dev/null; then
    HTTP_STATUS=$(curl -sL -w "%{http_code}" "$BINARY_URL" -o "$BINARY_PATH")
    if [ "$HTTP_STATUS" != "200" ]; then
        echo -e "${RED}❌ Error: Download failed with HTTP status $HTTP_STATUS${NC}"
        rm -f "$BINARY_PATH"
        exit 1
    fi
elif command -v wget &> /dev/null; then
    if ! wget -q -O "$BINARY_PATH" "$BINARY_URL"; then
        echo -e "${RED}❌ Error: Download failed${NC}"
        rm -f "$BINARY_PATH"
        exit 1
    fi
fi

# Verify the download was successful
if [ ! -s "$BINARY_PATH" ]; then
    echo -e "${RED}❌ Error: Downloaded file is empty${NC}"
    exit 1
fi

# Make binary executable
chmod +x "$BINARY_PATH"

# Install binary
echo -e "${BLUE}📦 Installing to $INSTALL_DIR/${BINARY_NAME}...${NC}"
mv "$BINARY_PATH" "$INSTALL_DIR/$BINARY_NAME"
chmod 755 "$INSTALL_DIR/$BINARY_NAME"

# Clean up
rm -rf "$TMP_DIR"

# Verify installation
if command -v $BINARY_NAME &> /dev/null; then
    echo -e "${GREEN}✅ tnl $VERSION has been successfully installed!${NC}"
    echo -e "${BLUE}Try it out with: ${YELLOW}$BINARY_NAME --help${NC}"
else
    echo -e "${YELLOW}The binary was installed to: $INSTALL_DIR/$BINARY_NAME${NC}"
    echo -e "${YELLOW}But it seems this directory is not in your PATH.${NC}"
    
    # Provide instructions for adding to PATH
    echo -e "\n${BLUE}To add this directory to your PATH, run:${NC}"
    
    if [[ "$SHELL" == */zsh ]]; then
        echo -e "${YELLOW}echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc${NC}"
        echo -e "${YELLOW}source ~/.zshrc${NC}"
    elif [[ "$SHELL" == */bash ]]; then
        echo -e "${YELLOW}echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.bashrc${NC}"
        echo -e "${YELLOW}source ~/.bashrc${NC}"
    else
        echo -e "${YELLOW}export PATH=\"$INSTALL_DIR:\$PATH\"${NC}"
        echo -e "${YELLOW}Add the above line to your shell's startup file.${NC}"
    fi
    
    echo -e "\n${BLUE}For now, you can run it directly with:${NC}"
    echo -e "${YELLOW}$INSTALL_DIR/$BINARY_NAME --help${NC}"
fi

# Add example usage
echo -e "\n${BLUE}Example usage:${NC}"
echo -e "${YELLOW}  # Start a tunnel server${NC}"
echo -e "  $BINARY_NAME serve"
echo -e "\n${YELLOW}  # Start a tunnel client${NC}"
echo -e "  $BINARY_NAME start myservice http://localhost:8080"
echo -e "\n${YELLOW}  # Update tnl to the latest version${NC}"
echo -e "  $BINARY_NAME update"