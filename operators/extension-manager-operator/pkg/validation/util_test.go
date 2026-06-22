/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validation

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_loadSchemaJSONFromFile_ValidFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "mock_schema.json.out")

	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(tmpFile.Name()); err != nil {
			fmt.Println("Failed to remove temp file:", err)
		}
	}()

	schemaJSONContent := getJSONSchemaFixture()
	if _, err := tmpFile.Write(schemaJSONContent); err != nil {
		t.Fatal(err)
	}

	if err := tmpFile.Close(); err != nil {
		t.Fatal(err)
	}

	schemaJSON, err := loadSchemaJSONFromFile(tmpFile.Name())

	assert.NoError(t, err)
	assert.NotNil(t, schemaJSON)
	assert.Equal(t, schemaJSONContent, schemaJSON)
}

func Test_loadSchemaJSONFromFile_InvalidFile(t *testing.T) {
	_, err := loadSchemaJSONFromFile("invalid_file_path")

	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err), "expected path error for missing file")
}

func Test_loadSchemaJSONFromFile_EmptyFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "mock_schema.json.out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(tmpFile.Name()); err != nil {
			fmt.Println("Failed to remove temp file:", err)
		}
	}()

	if err := tmpFile.Close(); err != nil {
		t.Fatal(err)
	}

	schemaJSON, err := loadSchemaJSONFromFile(tmpFile.Name())

	assert.NoError(t, err)
	assert.Equal(t, "", string(schemaJSON))
}
