package main

import (
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/manifoldco/promptui"
)

type Project struct {
	Repo string
}

func main() {
	projects := []Project{
		{Repo: "acceptance-fraud-engine"},
		{Repo: "acceptance-fx-api"},
		{Repo: "ecom-transaction-payments"},
		{Repo: "card-transaction-insights"},
		{Repo: "ecom-callback-gateway"},
		{Repo: "payments-refunds-wrapper"},
		{Repo: "kafka-secure-proxy"},
		{Repo: "consent-orchestrator-gateway"},
		{Repo: "acceptance-tap-onboarding"},
		{Repo: "teya-laime-helper"},
		{Repo: "gmd-crm-sync"},
		{Repo: "transaction-block-manager"},
		{Repo: "pricing-app-backend"},
		{Repo: "acceptance-aggregates-api"},
		{Repo: "commshub-sender-service"},
		{Repo: "iso-8583-proxy"},
		{Repo: "ecom-checkout-backend"},
		{Repo: "pricing-engine-service"},
		{Repo: "ecom-checkout-generator"},
		{Repo: "fake4-acquiring-host"},
	}

	fmt.Println("Project Selector")
	fmt.Println("================")

	selectedProjects, err := selectProjects(projects)
	if err != nil {
		log.Fatal("Project selection failed:", err)
	}

	if len(selectedProjects) == 0 {
		fmt.Println("No projects selected. Exiting.")
		return
	}

	fmt.Println("\nSelected projects:")
	for _, project := range selectedProjects {
		fmt.Printf("- %s\n", project.Repo)
	}

	fmt.Println("\nPlease enter the issue title:")
	titlePrompt := promptui.Prompt{
		Label: "Title",
	}

	issueTitle, err := titlePrompt.Run()
	if err != nil {
		log.Fatal("Failed to get title:", err)
	}

	if strings.TrimSpace(issueTitle) == "" {
		fmt.Println("No title provided. Exiting.")
		return
	}

	fmt.Println("\nPlease enter the issue description:")
	descriptionPrompt := promptui.Prompt{
		Label: "Description",
	}

	issueDescription, err := descriptionPrompt.Run()
	if err != nil {
		log.Fatal("Failed to get description:", err)
	}

	if strings.TrimSpace(issueDescription) == "" {
		fmt.Println("No description provided. Exiting.")
		return
	}

	fmt.Println("\nOpening GitHub issue creation pages in your browser...")
	fmt.Println("Please log in to GitHub if needed and submit the issues.")

	for _, project := range selectedProjects {
		err := openGitHubIssueInBrowser(project, issueTitle, issueDescription)
		if err != nil {
			log.Printf("Failed to open issue page for %s: %v", project.Repo, err)
		} else {
			fmt.Printf("✓ Opened issue page for %s\n", project.Repo)
		}
	}

	fmt.Println("\nDone!")
}

func selectProjects(projects []Project) ([]Project, error) {
	var selected []Project

	fmt.Println("\nAvailable projects:")
	for i, project := range projects {
		fmt.Printf("%d. %s\n", i+1, project.Repo)
	}

	prompt := promptui.Prompt{
		Label: "Enter project numbers separated by commas (e.g., 1,2) or 'all' for all projects",
	}

	input, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	input = strings.TrimSpace(input)

	if input == "" {
		return selected, nil
	}

	// Check if user wants to select all projects
	if strings.ToLower(input) == "all" {
		return projects, nil
	}

	indices := strings.Split(input, ",")
	for _, indexStr := range indices {
		indexStr = strings.TrimSpace(indexStr)
		index, err := strconv.Atoi(indexStr)
		if err != nil || index < 1 || index > len(projects) {
			fmt.Printf("Invalid selection: %s\n", indexStr)
			continue
		}

		project := projects[index-1]
		alreadySelected := false
		for _, sel := range selected {
			if sel.Repo == project.Repo {
				alreadySelected = true
				break
			}
		}

		if !alreadySelected {
			selected = append(selected, project)
		}
	}

	return selected, nil
}

func openGitHubIssueInBrowser(project Project, title string, description string) error {
	// URL encode the parameters
	encodedTitle := url.QueryEscape(title)
	body := url.QueryEscape(description)
	assignees := url.QueryEscape("copilot")

	// Construct the GitHub issue creation URL with assignee
	issueURL := fmt.Sprintf("https://github.com/saltpay/%s/issues/new?title=%s&body=%s&assignees=%s", project.Repo, encodedTitle, body, assignees)

	// Open the URL in the default browser
	return openBrowser(issueURL)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
