// Command demo is a minimal dialog showcase.
package main

import (
	"fmt"

	"github.com/malivvan/appkit/dialog"
)

func main() {
	chosen, err := dialog.Open(dialog.Options{
		Type:    dialog.TypeOpen,
		Title:   "Open a text file",
		Filters: []dialog.FileFilter{{Name: "Text files", Extensions: []string{"txt", "md", "log"}}},
	})
	if err != nil {
		fmt.Println("open:", err)
	} else {
		fmt.Printf("open       -> %v\n", chosen)
	}

	paths, err := dialog.Open(dialog.Options{Type: dialog.TypeOpenMultiple, Title: "Open several files"})
	if err != nil {
		fmt.Println("open multi:", err)
	} else {
		fmt.Printf("open multi -> %v\n", paths)
	}

	saveTo, err := dialog.Open(dialog.Options{
		Type:     dialog.TypeSave,
		Title:    "Save the report",
		Filename: "report.txt",
	})
	if err != nil {
		fmt.Println("save:", err)
	} else {
		fmt.Printf("save       -> %v\n", saveTo)
	}

	dir, err := dialog.Open(dialog.Options{Type: dialog.TypeDirectory, Title: "Choose an output directory"})
	if err != nil {
		fmt.Println("directory:", err)
	} else {
		fmt.Printf("directory  -> %v\n", dir)
	}

	fmt.Println("done")
}
