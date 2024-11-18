package actions

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func RunSearchAndReplaceAction(targetDir string) error {
	log.Println("🚚 We're applying action=search-replace-strings on targetDir=", targetDir)

	var searchString string
	log.Println("⚠️ Enter string to search and replace: ")
	_, err := fmt.Scanf("%s", &searchString)
	if err != nil {
		fmt.Println("(╯°□°)╯︵ ┻━┻ ", err)
		return err
	}
	var replacementString string
	log.Println("⚠️ Enter string to use as replacement: ")
	_, err = fmt.Scanf("%s", &replacementString)
	if err != nil {
		fmt.Println("(╯°□°)╯︵ ┻━┻ ", err)
		return err
	}

	// iterate all the files in the targetDir
	err = filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
		log.Println("Processing file ", path, "...")

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip not supported file extensions
		validFileExtensions := []string{".yml", ".yaml", ".properties", ".xml", ".java", ".json", ".txt", ".md", ".avsc"}
		if !slices.Contains(validFileExtensions, filepath.Ext(path)) {
			return nil
		}

		// Skip anything under target directory
		if strings.Contains(path, "target") {
			return nil
		}

		input, err := os.ReadFile(path)
		if err != nil {
			log.Fatalln(err)
		}

		lines := strings.Split(string(input), "\n")

		for i, line := range lines {
			// replace the string
			lines[i] = strings.ReplaceAll(line, searchString, replacementString)
		}
		output := strings.Join(lines, "\n")
		err = os.WriteFile(path, []byte(output), 0644)
		if err != nil {
			log.Fatalln(err)
			return err
		}
		return nil
	})

	return err
}
