#!/bin/zsh
set -e

BLUE='\033[0;34m'
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Declare target repositories
set -a repos "acceptance-fx-api" "acceptance-bin-service" "acquiring-payments-api" "transaction-block-aux" "transaction-block-manager"
set -a scripts "find-and-replacer" "yaml-changer"

# Function to display the change select menu
function show_script_menu() {
    echo "${BLUE} 😸 What would you like to do? ${NC}"
    local i=1
    for repo in "${(@k)repos}"; do
        echo "$i) $repo"
        ((i++))
    done
}

# Function to display the repo select menu
function show_repo_menu() {
    echo "${BLUE} 😸 Select the repositories you want to copy your changes to: ${NC}"
    local i=1
    for repo in "${(@k)repos}"; do
        echo "$i) $repo"
        ((i++))
    done
}

# Get user input
show_script_menu
echo "${BLUE} 😸 Choose an option: ${NC}"
read script_choice
show_repo_menu
echo "${BLUE} 😸 Choose your repos: ${NC}"
read repo_choices

selected_script=$script_choice
# Convert choices into an array
selected_repos=(${=repo_choices})

# Function to process each repository
function process_repo() {
    local repo_name=$1
    # Create branch with name copycat-YYYY-MM-DD-random
    local branch_name="copycat-$(date +%Y-%m-%d)-$RANDOM"

    if [ -d "../$repo_name" ]; then
        echo "${BLUE} 😸 Copying to $repo_name ... ${NC}"
        cd ../$repo_name
        # Reset repository to main branch
        git checkout . > /dev/null 2>&1 
        git checkout main > /dev/null 2>&1
        git pull > /dev/null 2>&1

        # Commit changes only on success
        if sh ../copycat/scripts/$selected_script.sh; then
            git checkout -b $branch_name > /dev/null 2>&1
            git add . > /dev/null 2>&1
            git commit -m "Copycat changes" > /dev/null 2>&1
            git push origin $branch_name > /dev/null 2>&1
            echo "${GREEN} 😸 Changes copied to $repo_name. Open a pull request at https://github.com/saltpay/$repo_name/pull/new/$branch_name ${NC}"
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

echo "${BLUE} 😸 Finished, thank you for using Copycat! ${NC}"
