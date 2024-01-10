#!/bin/zsh
set -e

#echo "setting offset based on shell version"
if [[ -n ${ZSH_VERSION} ]] && [[ ! -z ${ZSH_VERSION} ]]; then
  INDEX_OFFSET=0
else
  INDEX_OFFSET=1
fi

BLUE='\033[0;34m'
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

branch_name="copycat-$(date +%Y-%m-%d)-$RANDOM"

# Declare target repositories and scripts
repos=("acceptance-fx-api" "acceptance-bin-service" "acquiring-payments-api" "transaction-block-aux" "transaction-block-manager")
scripts=("find-and-replacer" "yaml-changer" "fetch-avro-schemas")

# Function to display the change select menu
function show_script_menu() {
    echo "${BLUE} 😸 What would you like to do? ${NC}"
    local i=1
    for script in "${scripts[@]}"; do
        echo "$i) $script"
        ((i++))
    done
}

function show_options_menu() {
    # Depending on the script chosen, show the options menu
    case $selected_script in
    "find-and-replacer")
        echo "${BLUE} 😸 Please enter the search string: ${NC}"
        read -r search_string
        echo "${BLUE} 😸 Please enter the replacement string: ${NC}"
        read -r replacement_string

        # Combining the inputs into options with flags
        options=(-f "$search_string" -r "$replacement_string")
        ;;
    "yaml-changer")
        echo "${BLUE} 😸 Please enter the target yaml file path: ${NC}"
        read -r filename
        echo "${BLUE} 😸 Please enter the target yaml key: ${NC}"
        read -r key
        echo "${BLUE} 😸 Please enter the target value: ${NC}"
        read -r value

        # Combining the inputs into options
        options=(-k "$key" -v "$value" -f "$filename")
        ;;
    "fetch-avro-schemas")
        # Used to remind the user to connect to the dev VPN
        echo "${BLUE} 😸 Are you connected to the dev VPN? y/n ${NC}"
        read -r vpn_choice
        ;;
    *) 
        options=()
        ;;
    esac
}

# Function to display the repo select menu
function show_repo_menu() {
    echo "${BLUE} 😸 Select the repositories you want to copy your changes to: ${NC}"
    local i=1
    for repo in "${repos[@]}"; do
        echo "$i) $repo"
        ((i++))
    done
}

# Prompts the user to choose a commit message for the changes
function show_commit_message_menu() {
    echo "${BLUE} 😸 Please enter a commit message: ${NC}"
    read -r commit_message
}

# Function to commit changes
function commit_changes() {
    if [ -n "$(git status --porcelain)" ]; then
        git add .
        git commit -m "$commit_message - Copycat ©" 
    else
        echo "No changes to commit."
    fi
}

# Function to process each repository
function process_repo() {
    local repo_name=$1

    if [ -d "../$repo_name" ]; then
        echo "${BLUE} 😸 Copying to $repo_name ... ${NC}"
        cd ../$repo_name

        # Commit changes only on success
        if sh ../copycat/scripts/$selected_script.sh "${options[@]}"; then
            commit_changes
        else
            echo "${RED} 😿 There was an error copying changes to $repo_name ${NC}"
        fi
    else
        echo "${RED} 😿 Could not find repository $repo_name ${NC}"
    fi
}

show_repo_menu
echo "${BLUE} 😸 Choose the repos you'd like to copy changes to: ${NC}"
read repo_choices
# Convert choices into an array
selected_repos=($repo_choices)

# Loop through selected repositories and reset to main branch
for index in "${selected_repos[@]}"; do
    repo_name=${repos[$index - $INDEX_OFFSET]}
    cd ../$repo_name
    git checkout main >/dev/null 2>&1
    git pull >/dev/null 2>&1
    git checkout -b $branch_name >/dev/null 2>&1
done

cd ../copycat

while true; do
    # Get user input
    show_script_menu
    echo "${BLUE} 😸 Choose an option: ${NC}"
    read script_choice

    selected_script=${scripts[$script_choice - $INDEX_OFFSET]}

    show_options_menu

    show_commit_message_menu

    # Loop through selected repositories and apply changes
    for index in "${selected_repos[@]}"; do
        repo_name=${repos[$index - 1]}
        process_repo $repo_name
    done

    # Ask user if they want to continue
    echo "${BLUE} 😸 Would you like to continue? (y/n) ${NC}"
    read continue_choice

    # If continue choice is not Y, break out of the loop
    if [[ ! $continue_choice =~ ^[Yy]$ ]]; then
        break
    fi
done

cd ../copycat

# Loop through selected repositories and push changes
for index in "${selected_repos[@]}"; do
    repo_name=${repos[$index - 1]}
    cd ../$repo_name
    git push origin $branch_name >/dev/null 2>&1
    echo "${GREEN} 😸 Changes copied to $repo_name. Open a pull request at https://github.com/saltpay/$repo_name/pull/new/$branch_name ${NC}"
done

echo "${BLUE} 😸 Finished, thank you for using Copycat ©! ${NC}"
