#!/bin/zsh
set -e

# ANSI color codes
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Initialize variables
findString=""
replaceString=""

# Function to display usage
usage() {
    echo "Usage: $0 -f <string-to-find> -r <string-to-replace-with>"
    echo "Example: $0 -f \"old string\" -r \"new string\""
    exit 1
}

# Parse command-line arguments
while getopts 'f:r:' flag; do
  case "${flag}" in
    f) findString="${OPTARG}" ;;
    r) replaceString="${OPTARG}" ;;
    *) usage ;;
  esac
done

# Assert that the script is being run from the root of the repository
if [ ! -d ".git" ]; then
    echo "${BLUE} 🦧 This script must be run from the root of the repository ${NC}"
    exit 1
fi

# Assert that the user has provided the required arguments
if [ -z "$findString" ] || [ -z "$replaceString" ]; then
    echo "${BLUE} 🦧 Please provide the string to find and the string to replace with ${NC}"
    usage
fi

echo "${BLUE} 🦧 Replacing $findString with $replaceString... ${NC}"

# Do the find and replace
find . -type f \
    -not -path "./.git/*" \
    -not -path "./.idea/*" \
    -not -path "./.vscode/*" \
    -not -path "./node_modules/*" \
    -not -path "./target/*" \
    -not -path "./.mvn/*" \
    -not -path "./certs/*" \
    -not -name ".DS_Store" \
    -not -name "*.jar" \
    -not -name "*.class" \
    -not -name "*.log" \
    -not -name "*.tmp" \
    -not -name "*.bak" \
    -not -name "*.swp" \
    -exec bash -c 'export LC_ALL=C; sed -i "" -e "s/$0/$1/g" "$2"' "$findString" "$replaceString" {} \;

sh ../copycat/scripts/bump-helm-chart.sh || true

echo "${BLUE} 🦧 Find and replace completed! ${NC}"
