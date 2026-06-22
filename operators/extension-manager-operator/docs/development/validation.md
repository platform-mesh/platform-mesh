## Using a `validation` library

To install the validation library, add the following import statement in your Go project:

```go
import "go.platform-mesh.io/extension-manager-operator/pkg/validation"
```

Example usage:

```go
package main

import (
    "fmt"
    "go.platform-mesh.io/extension-manager-operator/pkg/validation"
)

func main() {
    cC := validation.NewContentConfiguration()

    input := []byte(`{ "name": "example" }`)
    contentType := "json"

    result, err := cC.Validate(input, contentType)
    if err != nil {
        fmt.Println("Validation failed:", err)
    } else {
        fmt.Println("Validation succeeded:", result)
    }
}
```

## Using a `/validate` HTTP endpoint

```shell
# run with 'server' argument
go run main.go server

# validate docs/assets/test.json local file
curl http://localhost:8088/validate -X POST -d @docs/assets/test.json   -H "Content-Type: application/json"
```
