#!/bin/zsh
set -e

BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Define your repositories and their paths
declare -A repos
repos[transaction-block-manager]="/transaction-block-manager"
repos[transaction-block-aux]="/transaction-block-aux"
repos[acquiring-payments-api]="/acquiring-payments-api"
repos[acceptance-fx-api]="/acceptance-fx-api"
repos[acceptance-bin-service]="/acceptance-bin-service"

# Function to display the menu
function show_menu() {
    echo "${BLUE} 😸 Select the repositories you want to copy your changes to: ${NC}"
    local i=1
    for repo in "${(@k)repos}"; do
        echo "$i) $repo"
        ((i++))
    done
}

# Get user input
show_menu
echo "${BLUE} 😸 Enter selections: ${NC}"
read choices

# Convert choices into an array
selected_repos=(${=choices})

# Function to process each repository
function process_repo() {
    local repo_name=$1
    local repo_path=${repos[$repo_name]}

    if [ -d "../$repo_path" ]; then
        echo "${BLUE} 😸 Copying to $repo_name ... ${NC}"
        cd ../$repo_path

        if sh ../copycat/example-change.sh; then
            # Create branch with name copycat-YYYY-MM-DD-random
            local branch_name="copycat-$(date +%Y-%m-%d)-$RANDOM"
            git checkout -b $branch_name > /dev/null 2>&1
            git add . > /dev/null 2>&1
            git commit -m "Copycat changes" > /dev/null 2>&1
            git push origin $branch_name > /dev/null 2>&1
        else
            echo "${RED} 😿 There was an error copying changes to $repo_name${NC}"
        fi
    else
        echo "${RED} 😿 Could not find repository $repo_name ${NC}"
    fi
}

# Loop through selected repositories and process them
for index in $selected_repos; do
    repo_name=$(echo ${(@k)repos} | awk '{print $'"$index"'}')
    process_repo $repo_name
done

echo "${BLUE} 😸 Thank you for using Copycat! ${NC}"
