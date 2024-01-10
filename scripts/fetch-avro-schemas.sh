#!/bin/zsh
set -e

BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Simple script for fetching latest Avro schemas
# This script does not currently support updating major schema versions

# This script assumes it is being run from the root of the repository and user is connected to dev VPN
echo "${BLUE} 🦧 Fetching Avro schemas (this might take a while)... ${NC}"

./mvnw -pl "$(basename "$(pwd)")-kafka" io.confluent:kafka-schema-registry-maven-plugin:download # > /dev/null 2>&1

if [ $? -ne 0 ]; then
    echo "${BLUE} 🦧 Error fetching schemas for $(basename "$(pwd)")-kafka ${NC}"
    exit 1
fi

echo "${BLUE} 🦧 Done! ${NC}"
