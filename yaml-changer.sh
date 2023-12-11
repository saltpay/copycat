#!/bin/bash
set -e

BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "${BLUE} 🦧 Changes detected in k8s/chart... ${NC}"

# This script assumes it is being run from the root of the repository
yq e ".pdb.maxUnavailable = 2" k8s/chart/Chart.yaml -i
sh ../copycat/bump-helm-chart.sh
