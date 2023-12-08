package filesystem

import (
	"fmt"
	"log"
	"os"
)

const reposDir = "repos"

func DeleteEmptyWorkspace() {
	if err := os.Remove(reposDir); err == nil {
		fmt.Println("✓ Removed empty repos directory")
	}
}

func DeleteWorkspace() {
	if _, err := os.Stat(reposDir); err == nil {
		DeleteDirectory(reposDir)
	}
}

func CreateWorkspace() {
	// Create repos directory if it doesn't exist
	if err := os.MkdirAll(reposDir, 0755); err != nil {
		log.Fatal("Failed to create repos directory:", err)
	}
}

func DeleteDirectory(targetPath string) {
	fmt.Printf("Cleaning up %s...\n", targetPath)
	if err := os.RemoveAll(targetPath); err != nil {
		log.Printf("Warning: Failed to remove %s: %v", targetPath, err)
	} else {
		fmt.Printf("✓ Cleaned up %s\n", targetPath)
	}
}
