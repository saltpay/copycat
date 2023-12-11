#!/bin/bash
set -e

BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "${BLUE} 🦧 Running `yaml-changer.sh`... ${NC}"

# This script assumes it is being run from the root of the repository
yq e ".pdb.maxUnavailable = 2" k8s/chart/Chart.yaml -i
sh ../copycat/scripts/bump-helm-chart.sh

echo "${BLUE} 🦧 Done! ${NC}"