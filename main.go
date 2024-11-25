package main

import (
	"bufio"
	"copycat/internal/pkg/actions"
	"copycat/internal/pkg/recipes"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() {
	// array of strings foo and bar
	projects := []string{
		"acceptance-bin-service",
		"acceptance-aggregates-api",
		"acceptance-fx-api",
		"acceptance-fraud-engine",
		"acceptance-quality-control",
		"acceptance-otlp-collector",
		"acquiring-payments-api",
		"card-transaction-insights",
		"payments-gateway-service",
		"payments-refunds-wrapper",
		"demo-backend-service",
		"transaction-block-aux",
		"transaction-block-manager",
		"kafka-secure-proxy",
		"iso-8583-proxy",
		"fake4-acquiring-host"}

	defaultRecipes := []recipes.Recipe{
		{Type: "recipe", Name: "org.openrewrite.maven.UpdateMavenWrapper", DisplayName: "Update Maven Wrapper"},
		{Type: "action", Name: "search-replace-strings", DisplayName: "Search and replace string"},
		{Type: "action", Name: "update-avro-schemas", DisplayName: "Update avro schemas"},
	}

	log.Println("Welcome to copycat 2 😸!")
	log.Println("Please enter the project you want to copy changes to")
	log.Println("You can pick multiple projects by separating them with a space (e.g. 1 2 3)")

	// List all projects
	for i := 0; i < len(projects); i++ {
		log.Println(" - ", i, " ", projects[i])
	}

	// Ask user to select projects
	in := bufio.NewReader(os.Stdin)
	userInput, _ := in.ReadString('\n')
	userInput = strings.TrimSpace(userInput)

	// split the user input by spaces
	var selectedProjects []string
	indexesStr := strings.Split(userInput, " ")
	// convert the string to int
	for _, indexStr := range indexesStr {
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			fmt.Println("(╯°□°)╯︵ ┻━┻ ", err)
			return
		}
		selectedProjects = append(selectedProjects, projects[index])
	}

	log.Println("🥳 Congrats, you picked ", strings.Join(selectedProjects, ","))
	log.Println()

	log.Println("Please enter the change you want to apply")
	log.Println()

	allRecipes, err := getRecipes()
	if err != nil {
		log.Println(err)
		return
	}
	// append defaultRecipes to recipes
	for _, defaultRecipe := range defaultRecipes {
		allRecipes = append(allRecipes, defaultRecipe)
	}

	for index, recipe := range allRecipes {
		log.Println(" - ", index, " - ", recipe.DisplayName)
	}
	// Ask user to select projects
	in = bufio.NewReader(os.Stdin)
	userInput, _ = in.ReadString('\n')
	userInput = strings.TrimSpace(userInput)
	i, _ := strconv.Atoi(userInput)
	recipe := allRecipes[i]

	log.Println("Enter commit message: ")
	in = bufio.NewReader(os.Stdin)
	userInput, _ = in.ReadString('\n')
	commitMessage := strings.TrimSpace(userInput)

	for _, project := range selectedProjects {
		log.Println("🌟Validate target is clean ", project, "...")
		err = validate(project)
		if err != nil {
			log.Println("🚨 Error validating ", project, ": ", err)
			return
		}
		log.Println()
	}

	mapOfProjectToURL := make(map[string]string)
	for _, project := range selectedProjects {
		log.Println("🌟Copying changes to ", project, "...")
		pullRequestURL, err := runRecipe(project, recipe, commitMessage)

		if err != nil {
			log.Println("🚨 Error applyings changes to ", project, ": ", err)
			log.Println("🚨 Skipping to the next project...")
			mapOfProjectToURL[project] = "Error"
		} else {
			// add the url to the map
			mapOfProjectToURL[project] = pullRequestURL
		}
		log.Println()
	}

	log.Println("🐈 Copycat completed")
	for project, url := range mapOfProjectToURL {
		log.Println("🌟 Click the link to open pr for", project, "->", url)
	}
}

func validate(project string) error {
	currentDir, _ := os.Getwd()
	targetDir := strings.Replace(currentDir, "copycat", project, -1)

	log.Println("🚚 We're checking there are no uncommitted changes in the target project...")
	err := validateNoGitChanges(targetDir)
	if err != nil {
		return err
	}

	return nil
}

func runRecipe(project string, recipe recipes.Recipe, commitMessage string) (string, error) {
	currentDir, _ := os.Getwd()
	targetDir := strings.Replace(currentDir, "copycat", project, -1)

	log.Println("🚚 Switch to main branch...")
	err := switchToMainBranch(targetDir)
	if err != nil {
		return "", err
	}

	log.Println("🚚 Update main branch...")
	err = updateMainBranch(targetDir)
	if err != nil {
		return "", err
	}

	log.Println("🚚 We're creating a new git branch.")
	branch, err := gitCreateNewBranch(targetDir)
	if err != nil {
		log.Println(err)
		return "", err
	}

	if recipe.Type == "recipe" {
		log.Println("🚚 We're gonna copy rewrite.yaml to these projects.")
		err = copyRewrite(targetDir)
		if err != nil {
			log.Println(err)
			return "", err
		}

		log.Println("🚚 We're applying the recipe to the target projects (this may take a while...).")
		err = runMaven(targetDir, recipe)
		if err != nil {
			log.Println(err)
			return "", err
		}

		log.Println("🚚 We're deleting rewrite.yml from the target project.")
		err = deleteRewrite(targetDir)
		if err != nil {
			log.Println(err)
			return "", err
		}
	} else if recipe.Type == "action" {
		log.Println("🚚 We're applying the recipe to the target projects (this may take a while...).")
		err = runAction(targetDir, recipe)
		if err != nil {
			log.Println(err)
			return "", err
		}
	}

	log.Println("🚚 We're pushing the changes to a new git branch.")
	err = pushGitChanges(targetDir, *branch, commitMessage)
	if err != nil {
		return "", err
	}

	pullRequestURL := fmt.Sprintf("https://github.com/saltpay/%s/pull/new/%s", project, *branch)

	return pullRequestURL, nil
}

func runAction(targetDir string, action recipes.Recipe) error {
	log.Println("🚚 We're applying action=", action.Name, " on targetDir=", targetDir)

	if action.Name == "search-replace-strings" {
		return actions.RunSearchAndReplaceAction(targetDir)
	}
	return nil
}

func runMaven(targetDir string, recipe recipes.Recipe) error {
	app := "./mvnw"
	arg0 := "org.openrewrite.maven:rewrite-maven-plugin:run"
	arg1 := "-Drewrite.recipeArtifactCoordinates=org.openrewrite.recipe:rewrite-spring:LATEST"
	arg2 := "-Drewrite.activeRecipes=" + recipe.Name

	cmd := exec.Command(app, arg0, arg1, arg2)
	cmd.Dir = targetDir
	stdout, err := cmd.Output()
	if err != nil {
		log.Println("Error running maven: ", err)
		err := deleteRewrite(targetDir)
		if err != nil {
			return err
		}
		return err
	}

	log.Println(string(stdout))
	return nil
}

func gitCreateNewBranch(targetDir string) (*string, error) {
	branchName := "copycat-" + time.Now().Format("2006-01-02-15-04-05")

	cmd := exec.Command("git", "checkout", "-b", branchName)
	cmd.Dir = targetDir
	_, err := cmd.Output()
	if err != nil {
		log.Println("Error creating copycat branch: ", err)
		return nil, err
	}

	return &branchName, nil
}

func pushGitChanges(targetDir string, branchName string, commitMessage string) error {
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = targetDir
	_, err := cmd.Output()
	if err != nil {
		log.Println("Error running git add: ", err)
		return err
	}

	cmd = exec.Command("git", "commit", "-m "+commitMessage)
	cmd.Dir = targetDir
	_, err = cmd.Output()
	if err != nil {
		log.Println("Error running git commit: ", err)
		return err
	}

	cmd = exec.Command("git", "push", "origin", branchName)
	cmd.Dir = targetDir
	_, err = cmd.Output()
	if err != nil {
		log.Println("Error running git commit: ", err)
		return err
	}

	return nil
}

func switchToMainBranch(targetDir string) error {
	cmd := exec.Command("git", "switch", "main")
	cmd.Dir = targetDir
	_, err := cmd.Output()
	if err != nil {
		log.Println("🚨Error switching to main branch: ", err)
		return err
	}
	return nil
}

func updateMainBranch(targetDir string) error {
	cmd := exec.Command("git", "pull")
	cmd.Dir = targetDir
	_, err := cmd.Output()
	if err != nil {
		log.Println("🚨 Error updating main branch: ", err)
		return err
	}
	return nil
}

func validateNoGitChanges(targetDir string) error {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = targetDir
	stdout, err := cmd.Output()
	if err != nil {
		log.Println("Error running git status: ", err)
		return err
	}

	changes := strings.Split(string(stdout), "\n")

	var newChangesArray []string
	for _, change := range changes {
		// add all changes except empty lines and untracked files (??)
		if !strings.HasPrefix(change, "??") && change != "" {
			newChangesArray = append(newChangesArray, change)
		}
	}

	if len(newChangesArray) != 0 {
		// return error message foo
		return errors.New("🚨 Detected changes in the target project. Please stash or revert them before continuing.")
	}
	return nil
}

func copyRewrite(targetDir string) error {
	// delete rewrite.yml if it already exists
	err := deleteRewrite(targetDir)
	if err != nil {
		return err
	}

	// copy rewrite.yml to target project
	err = os.Link("rewrite.yml", targetDir+"/rewrite.yml")
	if err != nil {
		log.Println("Error copying file: ", err)
		return err
	}
	return nil
}

func deleteRewrite(targetDir string) error {
	// check if file exists
	if _, err := os.Stat(targetDir + "/rewrite.yml"); !os.IsNotExist(err) {
		err = os.Remove(targetDir + "/rewrite.yml")
		if err != nil {
			log.Println("Error deleting file: ", err)
			return err
		}
	}
	return nil
}

func getRecipes() ([]recipes.Recipe, error) {
	f, err := os.Open("rewrite.yml")
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(f)

	var allRecipes []recipes.Recipe
	for {
		spec := new(recipes.Recipe)
		err := decoder.Decode(&spec)
		if spec == nil {
			continue
		}
		// break loop on EOF
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		allRecipes = append(allRecipes, *spec)
	}

	return allRecipes, nil
}
