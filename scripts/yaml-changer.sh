#!/bin/bash
set -e

# Default values
key=""
value=""
filename=""

# ANSI color codes
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to display usage
usage() {
    echo "Usage: $0 -k <key> -v <value> -f <filename>"
    exit 1
}

# Parse command-line arguments
while getopts 'k:v:f:' flag; do
  case "${flag}" in
    k) key="${OPTARG}" ;;
    v) value="${OPTARG}" ;;
    f) filename="${OPTARG}" ;;
    *) usage ;;
  esac
done

# Check if all parameters are provided
if [ -z "$key" ] || [ -z "$value" ] || [ -z "$filename" ]; then
    echo "Error: Missing arguments"
    usage
fi

echo "${BLUE} 🦧 Running yaml-changer.sh... ${NC}"

# Change yaml file with provided key and value
yq e ".$key = \"$value\"" "$filename" -i
sh ../copycat/scripts/bump-helm-chart.sh

echo "${BLUE} 🦧 Done! ${NC}"
