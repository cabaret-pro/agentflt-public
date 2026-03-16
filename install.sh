#!/usr/bin/env bash
set -e

# agentflt installer
# Usage: curl -fsSL https://raw.githubusercontent.com/cabaret-pro/agentflt/main/install.sh | bash

REPO="cabaret-pro/agentflt"
BINARY_NAME="agentflt"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64|amd64)
        ARCH="amd64"
        ;;
    arm64|aarch64)
        ARCH="arm64"
        ;;
    *)
        echo -e "${RED}Error: Unsupported architecture $ARCH${NC}"
        exit 1
        ;;
esac

case "$OS" in
    darwin)
        OS="darwin"
        ;;
    linux)
        OS="linux"
        ;;
    *)
        echo -e "${RED}Error: Unsupported OS $OS${NC}"
        exit 1
        ;;
esac

echo -e "${BLUE}"
echo "██████╗  ██████╗ ███████╗███╗   ██╗████████╗███████╗██╗  ████████╗"
echo "██╔══██╗██╔════╝ ██╔════╝████╗  ██║╚══██╔══╝██╔════╝██║  ╚══██╔══╝"
echo "███████║██║  ███╗█████╗  ██╔██╗ ██║   ██║   █████╗  ██║     ██║   "
echo "██╔══██║██║   ██║██╔══╝  ██║╚██╗██║   ██║   ██╔══╝  ██║     ██║   "
echo "██║  ██║╚██████╔╝███████╗██║ ╚████║   ██║   ██║     ███████╗██║   "
echo "╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═╝  ╚═══╝   ╚═╝   ╚═╝     ╚══════╝╚═╝   "
echo "                                            agentflt · agent fleet"
echo -e "${NC}"
echo ""
echo -e "${GREEN}Installing agentflt...${NC}"
echo ""

# Check prerequisites
echo -e "${YELLOW}Checking prerequisites...${NC}"

# Check for tmux
if ! command -v tmux &> /dev/null; then
    echo -e "${RED}Error: tmux is not installed${NC}"
    echo ""
    echo "Please install tmux first:"
    echo "  macOS:   brew install tmux"
    echo "  Ubuntu:  sudo apt install tmux"
    echo "  Fedora:  sudo dnf install tmux"
    exit 1
fi
echo -e "${GREEN}✓ tmux found${NC}"

# Check for git
if ! command -v git &> /dev/null; then
    echo -e "${RED}Error: git is not installed${NC}"
    echo ""
    echo "Please install git first:"
    echo "  macOS:   brew install git"
    echo "  Ubuntu:  sudo apt install git"
    echo "  Fedora:  sudo dnf install git"
    exit 1
fi
echo -e "${GREEN}✓ git found${NC}"

# Check for Go (optional - for building from source)
if command -v go &> /dev/null; then
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    echo -e "${GREEN}✓ Go $GO_VERSION found${NC}"
    HAS_GO=1
else
    echo -e "${YELLOW}⚠ Go not found (will try to download binary)${NC}"
    HAS_GO=0
fi

echo ""

# Try to download pre-built binary (if available)
# For now, we'll build from source as there are no releases yet
if [ "$HAS_GO" -eq 0 ]; then
    echo -e "${RED}Error: Go is required to install agentflt${NC}"
    echo ""
    echo "Please install Go 1.21 or later:"
    echo "  https://golang.org/doc/install"
    echo ""
    echo "Or use your package manager:"
    echo "  macOS:   brew install go"
    echo "  Ubuntu:  sudo snap install go --classic"
    exit 1
fi

# Build from source
echo -e "${YELLOW}Building from source...${NC}"

TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

cd "$TEMP_DIR"
git clone --depth 1 "https://github.com/${REPO}.git" agentflt
cd agentflt

echo -e "${YELLOW}Compiling...${NC}"
go build -o "$BINARY_NAME" ./cmd/agentflt

# Create install directory if it doesn't exist
mkdir -p "$INSTALL_DIR"

# Install binary
echo -e "${YELLOW}Installing to $INSTALL_DIR...${NC}"
mv "$BINARY_NAME" "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/$BINARY_NAME"

# Check if install directory is in PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo ""
    echo -e "${YELLOW}⚠ Warning: $INSTALL_DIR is not in your PATH${NC}"
    echo ""
    echo "Add this line to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
    echo ""
    echo -e "${BLUE}  export PATH=\"\$PATH:$INSTALL_DIR\"${NC}"
    echo ""
fi

echo ""
echo -e "${GREEN}✓ Installation complete!${NC}"
echo ""
echo -e "${BLUE}Quick start:${NC}"
echo ""
echo "  # Create your first agent"
echo "  agentflt new --title \"My Agent\" --type claude --repo . --cmd \"claude\""
echo ""
echo "  # Open the dashboard"
echo "  agentflt dashboard"
echo ""
echo -e "${BLUE}Documentation:${NC}"
echo "  https://github.com/${REPO}"
echo ""
echo -e "${GREEN}Happy agent wrangling! 🚀${NC}"
