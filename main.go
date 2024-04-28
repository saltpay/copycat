package main

import (
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
		"acceptance-otlp-collector",
		"acceptance-quality-control",
		"acquiring-payments-api",
		"card-transaction-insights",
		"payments-gateway-service",
		"payments-refunds-wrapper",
		"demo-backend-service",
		"transaction-block-aux",
		"transaction-block-manager",
		"transaction-block-janitor",
		"kafka-secure-proxy",
		"fake4-acquiring-host"}

	defaultRecipes := []recipe{
		{Type: "recipe", Name: "org.openrewrite.maven.UpdateMavenWrapper", DisplayName: "Update Maven Wrapper"},
	}

	log.Println("Welcome to copycat 2 😸!")
	log.Println("Please enter the project you want to copy changes to")
	log.Println("You can pick multiple projects by separating them with a comma (e.g. 1,2,3)")

	// List all projects
	for i := 0; i < len(projects); i++ {
		log.Println(" - ", i, " ", projects[i])
	}

	// instead of getting just one number, getting a list
	var selectedProjects []string
	var list string
	// get indexes by separated by a comma
	_, err := fmt.Scanf("%s", &list)
	if err != nil {
		fmt.Println("(╯°□°)╯︵ ┻━┻ ", err)
		return
	}
	// split the list by comma
	indexesStr := strings.Split(list, ",")
	// convert the string to int
	for _, indexStr := range indexesStr {
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			fmt.Println("(╯°□°)╯︵ ┻━┻ ", err)
			return
		}
		selectedProjects = append(selectedProjects, projects[index])
	}

	log.Println("🥳 Congrats mate, you picked ", strings.Join(selectedProjects, ","))
	log.Println()

	log.Println("Please enter the change you want to apply")
	log.Println()

	recipes, err := getRecipes()
	if err != nil {
		log.Println(err)
		return
	}
	// append defaultRecipes to recipes
	for _, defaultRecipe := range defaultRecipes {
		recipes = append(recipes, defaultRecipe)
	}

	var i = 0
	for i, recipe := range recipes {
		log.Println(" - ", i, " ", recipe.Name, " - ", recipe.DisplayName)
	}
	_, err = fmt.Scanf("%d", &i)
	if err != nil {
		fmt.Println("(╯°□°)╯︵ ┻━┻ ", err)
		return
	}

	recipe := recipes[i]

	var commitMessage string
	log.Println("And the commit message you want to use: ")
	_, err = fmt.Scanf("%s", &commitMessage)
	if err != nil {
		fmt.Println("(╯°□°)╯︵ ┻━┻ ", err)
		return
	}

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
			log.Println("🚨 Error copying changes to ", project, ": ", err)
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

func runRecipe(project string, recipe recipe, commitMessage string) (string, error) {
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

	log.Println("🚚 We're pushing the changes to a new git branch.")
	err = pushGitChanges(targetDir, *branch, commitMessage)
	if err != nil {
		return "", err
	}

	pullRequestURL := fmt.Sprintf("https://github.com/saltpay/%s/pull/new/%s", project, *branch)

	return pullRequestURL, nil
}

func runMaven(targetDir string, recipe recipe) error {
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
	cmd := exec.Command("git", "checkout", "main")
	cmd.Dir = targetDir
	_, err := cmd.Output()
	if err != nil {
		log.Println("Error running git checkout: ", err)
		return nil, err
	}

	cmd = exec.Command("git", "pull")
	cmd.Dir = targetDir
	_, err = cmd.Output()
	if err != nil {
		log.Println("Error running git pull: ", err)
		return nil, err
	}

	branchName := "copycat-" + time.Now().Format("2006-01-02-15-04-05")

	cmd = exec.Command("git", "checkout", "-b", branchName)
	cmd.Dir = targetDir
	_, err = cmd.Output()
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
		log.Println("🚨Error updating main branch: ", err)
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

func getRecipes() ([]recipe, error) {
	f, err := os.Open("rewrite.yml")
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(f)

	var recipes []recipe
	for {
		spec := new(recipe)
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
		recipes = append(recipes, *spec)
	}

	return recipes, nil
}

type recipe struct {
	Type        string `yaml:"type"`
	Name        string `yaml:"name"`
	DisplayName string `yaml:"displayName"`
}
