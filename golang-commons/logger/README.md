# Platform Mesh Logger

The Platform Mesh logger is a structured type safe logger that operates on top of [Zerolog](https://github.com/rs/zerolog).
It can create JSON formatted logs but also tagged logs for console output.

### Initialization

Create a logger instance with the `New` function and pass it to functions and structs that need it.
Please avoid using a global variable logger. Instead create an instance or a component logger that inherits from an existing logger.

```go
// DefaultConfig() returns a config with defaults set
logConfig := logger.DefaultConfig()

// set config values
logConfig.Name = "my-service"
logConfig.Level = "debug"

// ceate logger
log, err := logger.New(logConfig)
if err != nil {
	fmt.Println("failed to create logger: %s", err)
    os.Exit(1)
}

```

### Configuration

You can use `DefaultConfig()` to create a configuration with preset defaults that can be changed.
But it is also possible to initialize the `Config{}` struct directly. Please make sure to set an output (default is `os.Stdout`).

```go
type Config struct {
	Name   string
	Level  string
	NoJSON bool
	Output io.Writer
}
```
|Field| Description
|----------|------------------------------------------------------|
|`Name` | Defines a name field that is appended to every log entry.|
|`Level` | Sets the minimal level for printing log messages. Can be a string of debug, info, error |
|`NoJSON` | Turns off JSON output. This is useful for local debugging as it is more human read-able.|
|`Output` | Output for log messages. Must be an `io.Writer`. Default is `io.Stdout`|

For testing it is possible to pass a `&bytes.Buffer{}` as `Output` to collect logs in a buffer and not print it on stdout.

### Usage

The logger uses a chained syntax for creating log entrys.
You start with the level you want to log in, then optional fields that further describe the context of an error and finally the message itself.

This will create a log entry with level fatal, sets the error as a field in the log output and prints the message "init failed":
```go
log.Fatal().Err(err).Msg("Init failed")
```

Fatal is a special level that logs and exits the program.

The following example creates a log entry of type info with several structured fields:
```go
log.Info().Int("count", count).Int("total", number).Str("tenant", tenantID).Msg("Init users")
```

You can add as many fields as needed. There are functions for all Go types including `interface{}`.
These fields are logged as JSON fields and can be used in Kibana to select messages.

There is a special field function `Err(error)` for handling errors. Please use this
to log errors like this:
```go
log.Error().Err(err).Msg("Failed to load data")
```

Fields are optionally and can be omitted, the Msg call instead is obligatory:
```go
log.Debug().Msg("Service started")
```

Never forget to call `Msg()` otherwise no log entry is created!
There is also a `Msgf(format, values...)` function but try to use fields if possible and create a
static log message as it makes creating dashboards in Kibana easier.

### Child Logger and Component Logger

There is a helper method `log.ChildLogger(key string, value string)` that returns a new logger with an added
field `key` with the value `value`. This can be used to create child loggers that inherit from a given main logger but
always include this pre-set field.

This can be handy if a logger is used several times in a function and should for instance always
contain the name of the function.

The helper method `log.ComponentLogger(component string)` is a special version of a child logger that returns a new logger with an added
field `component`. The idea behind this is to create a logger for a component like a specific part of an application
and give this field a common name that can be used for in Kibana.

### Logr Instance

The helper method `log.Logr()` returns a log instance of an existing Platform Mesh Logger that fulfills the `logr.Logger` interface from [go-logr](https://github.com/go-logr/logr).
This is a common interface that is used in many external packages for instance in the Kubernetes controller runtime.

The returned logger inherits settings from the existing Platform Mesh Logger.


### Default Logger

The package defines a global default logger as `logger.StdLogger`. Please only use it when really needed, e.g. in case of refactoring old code.
Please always create a new logger instance with `New()` and pass it to structs and functions.
This makes testing easier and is in general best practice.

### Platform Mesh Logger from Zerolog

Because the Platform Mesh logger internally embeds Zerolog it is compatible with new versions of Zerolog.
Nevertheless, because of this embeding, some functions return Zerolog instances.
It is very easy to return a new Platform Mesh logger from a Zerolog instance using this helper function:
```go
log := NewFromZerolog(zerologger)
```

### HTTP Middleware

The logger comes with a HTTP middleware that injects the logger into a context and a helper function to load it from a given context.
The middleware is compatible with any Go stdlib compatible router (e.g. `http.Mux` or Chi).

```go
// create log as logger instance and inject it
router.Use(logger.StoreLoggerMiddleware(log))

// get it from a request context
func(w http.ResponseWriter, r *http.Request) {
    log := LoadFromContext(r.Context())
}
```

If no logger can be found in the context, `LoadFromContext()` returns `logger.StdLogger`.
