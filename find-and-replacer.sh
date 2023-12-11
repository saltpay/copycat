#!/bin/zsh
set -e

BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Finds all instances of a string within the repo and replaces it with another string
# Usage: ./find-and-replacer.sh <string-to-find> <string-to-replace-with>
# Example: ./find-and-replacer.sh "acquiring" "acceptance"

# Assert that the script is being run from the root of the repository
if [ ! -d ".git" ]; then
    echo "${BLUE} 🦧 This script must be run from the root of the repository ${NC}"
    exit 1
fi

# Assert that the user has provided the required arguments
if [ -z "$1" ] || [ -z "$2" ]; then
    echo "${BLUE} 🦧 Please provide the string to find and the string to replace with ${NC}"
    exit 1
fi

# Assert that the string to find is not empty
if [ -z "$1" ]; then
    echo "${BLUE} 🦧 Please provide the string to find ${NC}"
    exit 1
fi

# Do the find and replace
find . -type f -not -path "./.git/*" -not -path "./.idea/*" -not -path "./.vscode/*" -not -path "./node_modules/*" -not -path "./mvn/*" -not -path "./certs/*" -exec sed -i '' -e "s/$1/$2/g" {} \;
