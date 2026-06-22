package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/invopop/jsonschema"

	"go.platform-mesh.io/extension-manager-operator/pkg/validation"
)

func reflectContentConfiguration() {
	r := new(jsonschema.Reflector)
	// r.ExpandedStruct = false
	schemaCategory := r.Reflect(&validation.Category{})
	schemaWebcomponent := r.Reflect(&validation.Webcomponent{})
	schemaRoot := r.Reflect(&validation.ContentConfiguration{})
	schemaRoot.Definitions["Category"] = schemaCategory.Definitions["Category"]
	schemaRoot.Definitions["Webcomponent"] = schemaWebcomponent.Definitions["Webcomponent"]
	data, err := json.MarshalIndent(schemaRoot, "", "  ")
	if err != nil {
		panic(err.Error())
	}

	// Get the directory of the current file
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Println("Unable to get the current file path")
		return
	}
	currentDir := filepath.Dir(currentFile)
	targetPath := filepath.Join(currentDir, "schema_autogen.json")

	// Save the schema to a file
	file, err := os.Create(targetPath)
	if err != nil {
		panic(err.Error())
	}
	defer func() {
		err := file.Close()
		if err != nil {
			panic(err)
		}
	}()

	_, err = file.Write(data)
	if err != nil {
		panic(err.Error())
	}
	err = file.Sync()
	if err != nil {
		panic(err.Error())
	}
	// // print data
	// fmt.Println(string(data))

	fmt.Println("Schema saved to schema_autogen.json")
}

func main() {
	reflectContentConfiguration()
}
