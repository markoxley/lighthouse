# Lighthouse - High-Performance Go Logging Module

[![Go Report Card](https://goreportcard.com/badge/github.com/markoxley/lighthouse)](https://goreportcard.com/report/github.com/markoxley/lighthouse)
[![GoDoc](https://godoc.org/github.com/markoxley/lighthouse?status.svg)](https://godoc.org/github.com/markoxley/lighthouse)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Author: Mark Oxley**

A production-ready, high-performance logging module for Go applications featuring asynchronous buffered logging, thread-safe operations, structured logging with template substitution, and multi-database persistence support.

## Features

- **Asynchronous Buffered Logging** - Non-blocking log calls with a 10,000 entry buffer for high-throughput applications
- **Thread-Safe** - Full concurrency support with mutex protection for all operations
- **Structured Logging** - Template-based placeholders: `lighthouse.Info("User {user} logged in", username)`
- **Multiple Log Levels** - Debug, Info, Warn, Error, Panic, Fatal with level filtering
- **Colored Console Output** - ANSI color-coded output by log level for better readability
- **External Log Handlers** - Custom callback functions for integration with monitoring systems
- **Database Persistence** - Built-in support for SQLite, PostgreSQL, MySQL, MSSQL, and Oracle
- **Graceful Shutdown** - Automatic buffer flushing on SIGINT/SIGTERM signals
- **Standard Library Compatible** - Implements `io.Writer` interface for seamless integration
- **98%+ Test Coverage** - Comprehensive test suite with race condition testing

## Installation

```bash
go get github.com/markoxley/lighthouse
```

### Database Support (Optional)

For SQLite support (used in tests and examples):

```bash
go get github.com/mattn/go-sqlite3
```

## Quick Start

### Basic Usage

```go
package main

import (
    "github.com/markoxley/lighthouse"
)

func main() {
    // Simple logging with template substitution
    lighthouse.Info("Application started on port {port}", 8080)
    lighthouse.Debug("Debug information: {detail}", "connection established")
    
    // Structured error logging
    lighthouse.Error("Failed to process request: {error}", err.Error())
}
```

### Setting Log Level

```go
// Only log Warn and above (filter out Debug and Info)
lighthouse.SetLevel(lighthouse.LogLevelWarn)

// Log everything including Debug
lighthouse.SetLevel(lighthouse.LogLevelDebug)
```

### Template-Based Logging

```go
// Templates use {placeholder} syntax
lighthouse.Info("User {username} ({userId}) performed {action} on {resource}", 
    "alice", 
    12345, 
    "UPDATE", 
    "orders")

// Output: User alice (12345) performed UPDATE on orders
```

## Advanced Usage

### Custom Log Handler

Send logs to external services like monitoring APIs or message queues:

```go
lighthouse.SetOutput(func(level lighthouse.LogLevel, dt time.Time, message string, params map[string]string) {
    // Send to monitoring service
    monitoringClient.SendLog(monitoring.LogEntry{
        Level:     level.String(),
        Timestamp: dt,
        Message:   message,
        Metadata:  params,
    })
    
    // Send to Slack for errors
    if level >= lighthouse.LogLevelError {
        slackClient.PostMessage("#alerts", message)
    }
})
```

### Database Persistence

Persist logs to any supported database:

```go
package main

import (
    "database/sql"
    "github.com/markoxley/lighthouse"
    _ "github.com/mattn/go-sqlite3"
)

func main() {
    // Open database connection
    db, err := sql.Open("sqlite3", "./app.db")
    if err != nil {
        panic(err)
    }
    defer db.Close()
    
    // Configure database logging with custom table name
    err = lighthouse.SetDatabase(lighthouse.LogDBSQLite, db, "application_logs")
    if err != nil {
        lighthouse.Error("Failed to setup database logging: {error}", err.Error())
    }
    
    // All subsequent logs will be written to both console and database
    lighthouse.Info("Database logging enabled")
}
```

#### Supported Databases

| Database   | Type Constant     | Driver                             | Placeholder Style |
| ---------- | ----------------- | ---------------------------------- | ----------------- |
| SQLite     | `LogDBSQLite`     | `github.com/mattn/go-sqlite3`      | `?`               |
| PostgreSQL | `LogDBPostgreSQL` | `github.com/lib/pq`                | `$1`              |
| MySQL      | `LogDBMySQL`      | `github.com/go-sql-driver/mysql`   | `?`               |
| MSSQL      | `LogDBMSSQL`      | `github.com/denisenkom/go-mssqldb` | `@p1`             |
| Oracle     | `LogDBOracle`     | `github.com/godror/godror`         | `:1`              |

### Database Schema

The module automatically creates the following table structure:

```sql
CREATE TABLE logs (
    timeStamp        INTEGER/BIGINT/NUMBER,  -- Unix timestamp
    level            INTEGER,                  -- Log level (0-5)
    levelDescription TEXT,                     -- "DEBUG", "INFO", etc.
    message          TEXT,                     -- Log message
    params           TEXT                      -- JSON-encoded structured params
);

CREATE INDEX idx_logs_ts ON logs (timeStamp);
```

### Standard Library Integration

Use Lighthouse as the output for Go's standard `log` package:

```go
package main

import (
    "log"
    "github.com/markoxley/lighthouse"
)

func init() {
    // Redirect standard library logs to Lighthouse
    // This is done automatically when importing the package
    // but can be configured explicitly:
    log.SetOutput(lighthouse.Logger)
}

func main() {
    // These will now go through Lighthouse with proper formatting
    log.Println("Standard library log message")
}
```

### Formatted Logging

For printf-style formatting, use the `*f` variants:

```go
lighthouse.Infof("Processing %d items with %.2f%% success rate", count, rate)
lighthouse.Debugf("Request took %v", duration)
lighthouse.Errorf("Operation failed: %v", err)
```

## Log Levels

| Level | Value | Color   | Description                                     |
| ----- | ----- | ------- | ----------------------------------------------- |
| Debug | 0     | Blue    | Detailed troubleshooting information            |
| Info  | 1     | Green   | General operational status                      |
| Warn  | 2     | Yellow  | Potential issues, operation continues           |
| Error | 3     | Red     | Failures that prevented operation               |
| Panic | 4     | Magenta | Critical failures, triggers panic after logging |
| Fatal | 5     | Red     | Critical errors, calls os.Exit(1) after logging |

## Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Application   │───▶│ Buffered Channel │───▶│ processBuffer() │
│   (goroutines)  │     │  (10,000 slots)  │     │  (1 goroutine)  │
└─────────────────┘     └──────────────────┘     └────────┬────────┘
                                                          │
                                        ┌─────────────────┼───────────────────┐
                                        │                 │                   │
                                        ▼                 ▼                   ▼
                                    ┌─────────┐   ┌────────────────┐   ┌────────────┐
                                    │ Console │   │ Custom Handler │   │ Database   │
                                    │(colored)│   │  (optional)    │   │ (optional) │
                                    └─────────┘   └────────────────┘   └────────────┘
```

### Thread Safety

All public functions are thread-safe:
- `SetLevel()`, `SetOutput()`, `SetDatabase()` use mutex protection
- `Debug()`, `Info()`, etc. use non-blocking channel sends
- Console output is synchronized via mutex
- Database writes happen in the single processBuffer goroutine

### Graceful Shutdown

The module automatically handles SIGINT and SIGTERM:

1. Signal received
2. `flush()` closes the buffer channel
3. `processBuffer()` processes remaining items
4. `wg.Wait()` blocks until all logs are written
5. Application exits cleanly

## Testing

Run the comprehensive test suite:

```bash
# Run all tests
go test -v ./...

# Run with coverage
go test -cover ./...

# Run with race detector
go test -race ./...

# Run benchmarks
go test -bench=. ./...
```

## Performance

Benchmarks on a typical development machine:

```
BenchmarkLog-8              1000000    1050 ns/op    0 B/op    0 allocs/op
BenchmarkParseMessage-8      5000000     325 ns/op   48 B/op    3 allocs/op
BenchmarkSanitizeTableName-8 10000000   112 ns/op    0 B/op    0 allocs/op
```

The buffered channel design ensures that logging calls return immediately (non-blocking) in normal operation, only blocking if the 10,000 entry buffer fills up.

## API Reference

### Logging Functions

```go
// Template-based logging (recommended)
func Debug(message string, args ...interface{})
func Info(message string, args ...interface{})
func Warn(message string, args ...interface{})
func Error(message string, args ...interface{})
func Panic(message string, args ...interface{})  // Triggers panic after logging
func Fatal(message string, args ...interface{})  // Exits with code 1 after logging

// Formatted logging (printf-style)
func Debugf(message string, args ...interface{})
func Infof(message string, args ...interface{})
func Warnf(message string, args ...interface{})
func Errorf(message string, args ...interface{})
func Panicf(message string, args ...interface{})
func Fatalf(message string, args ...interface{})
```

### Configuration Functions

```go
// Set minimum log level (Debug=0, Info=1, Warn=2, Error=3, Panic=4, Fatal=5)
func SetLevel(level LogLevel)

// Set custom handler for log entries
func SetOutput(logFunc LogFunc)

// Configure database persistence
func SetDatabase(dbType LogDB, db *sql.DB, table ...string) error
```

### Types

```go
// LogLevel is an int type with iota constants
type LogLevel int
const (
    LogLevelDebug LogLevel = iota
    LogLevelInfo
    LogLevelWarn
    LogLevelError
    LogLevelPanic
    LogLevelFatal
)

// LogDB identifies database types
type LogDB int
const (
    LogDBSQLite LogDB = iota
    LogDBPostgreSQL
    LogDBMySQL
    LogDBMSSQL
    LogDBOracle
)

// LogFunc is the handler callback signature
type LogFunc func(level LogLevel, dt time.Time, message string, params map[string]string)
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request. For major changes, please open an issue first to discuss what you would like to change.

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Acknowledgments

- Inspired by the need for a simple yet powerful logging solution in Go
- Built with production use cases in mind (high throughput, graceful shutdown, database persistence)
- Comprehensive test coverage ensures reliability
