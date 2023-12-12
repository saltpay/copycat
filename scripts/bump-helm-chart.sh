#!/bin/zsh
set -e

BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Assert that yq is installed
if ! command -v yq &>/dev/null; then
    echo "${BLUE} 🦧 yq could not be found ${NC}"
    echo "${BLUE} 🦧 Please install yq using the instructions at https://mikefarah.gitbook.io/yq/#install ${NC}"
    exit 1
fi

# This script assumes it is being run from the root of the repository
# Check if there are changes in the k8s/chart folder
if git diff --name-only origin/main | grep -q "k8s/chart"; then
    echo "${BLUE} 🦧 Changes detected in k8s/chart... ${NC}"

    new_version=$(echo $(yq e '.version' k8s/chart/Chart.yaml) | awk -F. -v OFS=. '{$3=$3+1;print}')

    yq e ".helm.version = \"$new_version\"" .cicd/deployment.yaml -i
    yq e ".version = \"$new_version\"" k8s/chart/Chart.yaml -i

    echo "${BLUE} 🦧 Chart version updated to $new_version ${NC}"
else
    echo "${BLUE} 🦧 No changes in k8s/chart ${NC}"
    exit 2
fi
