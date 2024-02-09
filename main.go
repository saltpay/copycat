package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

func main() {
	// array of strings foo and bar
	projects := []string{
		"acceptance-bin-service",
		"acceptance-fx-api",
		"acceptance-otlp-collector"}

	log.Println("Welcome to copycat 2 😸!")
	log.Println("Please enter the project you want to copy changes to")

	// List all projects
	for i := 0; i < len(projects); i++ {
		log.Println(" - ", i, " ", projects[i])
	}

	var i int
	_, err := fmt.Scanf("%d", &i)
	if err != nil {
		fmt.Println("(╯°□°)╯︵ ┻━┻ ", err)
		return
	}

	log.Println("🥳 Congrats mate, you picked", projects[i])
	log.Println()

	currentDir, _ := os.Getwd()
	targetDir := strings.Replace(currentDir, "copycat", projects[i], -1)

	log.Println("🚚 We're checking there are no uncommitted changes in the target project.")
	err = validateNoGitChanges(targetDir)
	if err != nil {
		log.Println(err)
		return
	}

	log.Println("🚚 We're creating a new git branch.")
	err = gitCreateNewBranch(targetDir)
	if err != nil {
		log.Println(err)
		return
	}

	log.Println("🚚 We're gonna copy rewrite.yaml to these projects.")
	err = copyRewrite(targetDir)
	if err != nil {
		log.Println(err)
		return
	}

	log.Println("🚚 We're applying the recipe to the target projects (this may take a while...).")
	err = runMaven(targetDir)
	if err != nil {
		log.Println(err)
		return
	}

	log.Println("🚚 We're deleting rewrite.yml from the target project.")
	err = deleteRewrite(targetDir)
	if err != nil {
		log.Println(err)
		return
	}

	log.Println("🚚 We're pushing the changes to a new git branch.")
	err = pushGitChanges(targetDir)
	if err != nil {
		return
	}
}

func runMaven(targetDir string) error {
	app := "./mvnw"
	arg0 := "org.openrewrite.maven:rewrite-maven-plugin:run"
	arg1 := "-Drewrite.recipeArtifactCoordinates=org.openrewrite.recipe:rewrite-spring:LATEST"

	// TODO: user should be able to pick what recipe to run or maybe we can just read the rewrite.yml and run all of them?
	arg2 := "-Drewrite.activeRecipes=com.teya.AddSpringPropertyExample"

	cmd := exec.Command(app, arg0, arg1, arg2)
	cmd.Dir = targetDir
	stdout, err := cmd.Output()
	if err != nil {
		err := deleteRewrite(targetDir)
		if err != nil {
			return err
		}
		return err
	}

	log.Println(string(stdout))
	return nil
}

func gitCreateNewBranch(targetDir string) error {
	cmd := exec.Command("git", "checkout", "main")
	cmd.Dir = targetDir
	_, err := cmd.Output()
	if err != nil {
		log.Println("Error running git checkout: ", err)
		return err
	}

	cmd = exec.Command("git", "pull")
	cmd.Dir = targetDir
	_, err = cmd.Output()
	if err != nil {
		log.Println("Error running git pull: ", err)
		return err
	}

	cmd = exec.Command("git", "checkout", "-b", "copycat")
	cmd.Dir = targetDir
	_, err = cmd.Output()
	if err != nil {
		log.Println("Error creating copycat branch: ", err)
		return err
	}

	return nil
}

func pushGitChanges(targetDir string) error {
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = targetDir
	_, err := cmd.Output()
	if err != nil {
		log.Println("Error running git add: ", err)
		return err
	}

	cmd = exec.Command("git", "commit", "-m Copycat")
	cmd.Dir = targetDir
	_, err = cmd.Output()
	if err != nil {
		log.Println("Error running git commit: ", err)
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
